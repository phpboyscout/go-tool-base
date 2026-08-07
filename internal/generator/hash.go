package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
)

// mergeHashes returns a new map containing all entries from base, with entries
// from overrides taking precedence. Neither input map is modified.
func mergeHashes(base, overrides map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(overrides))

	for k, v := range base {
		merged[k] = v
	}

	for k, v := range overrides {
		merged[k] = v
	}

	return merged
}

func calculateHash(content []byte) string {
	hash := sha256.Sum256(content)

	return hex.EncodeToString(hash[:])
}

// resolveCommandFileConflict decides what to do with an existing generated
// command file (cmd.go, init.go, main_test.go) whose stored hash lives on the
// manifest command entry rather than in the project-level Hashes map.
//
// It is the command path's thin caller of the shared resolver: it looks the
// stored hash up in the right namespace, converts the path to the
// project-relative form ignore rules match against, and returns the hash the
// manifest should carry.
//
// A declined file yields Write() == false rather than an error. Signalling
// "keep the developer's file" as an error is what used to unwind the whole
// regeneration at the first conflict (issue #13).
func (g *Generator) resolveCommandFileConflict(fullPath string, newContent []byte) conflictDecision {
	relPath := g.relProjectPath(fullPath)

	// A file that is not there yet cannot conflict, and looking up its stored
	// hash means decoding the manifest. Skip that for a fresh write — but only
	// once an ignore rule is ruled out, since an ignored path is never written
	// even when it is absent.
	if exists, _ := afero.Exists(g.props.FS, fullPath); !exists && !g.ignoreRules().IsIgnored(relPath) {
		return conflictDecision{Outcome: conflictWrite}
	}

	var storedHash string

	if cmd, err := g.findManifestCommand(); err == nil && cmd != nil {
		filename := filepath.Base(fullPath)

		storedHash = cmd.Hashes[filename]
		if storedHash == "" && filename == "cmd.go" {
			storedHash = cmd.Hash
		}
	}

	return g.resolveConflict(fullPath, relPath, storedHash, newContent)
}

// promptOverwrite returns true if the file at path should be overwritten.
// existing and newContent are optional; when both are provided the user can
// choose to view a full-screen diff before deciding.
func (g *Generator) promptOverwrite(path string, existing, newContent []byte) bool {
	switch g.config.Overwrite {
	case "allow":
		return true
	case "deny":
		return false
	}

	// Default: ask. Never attempt a terminal prompt in a non-interactive context.
	// A headless/CI run has no controlling TTY, so huh would fail opening
	// /dev/tty and emit a per-file, stack-flavoured "Prompt failed" warning
	// (issue #6.2). Detect it up front and resolve by the safe default — skip —
	// mirroring the pkg/cmd/root pre-run prompt TTY guard.
	if g.isNonInteractive() {
		return false
	}

	return g.askOverwriteAction(path, existing, newContent)
}

// askOverwriteAction presents an interactive select to the user and returns
// their decision. When both existing and newContent are non-nil the user can
// also choose to view a full-screen diff before deciding.
func (g *Generator) askOverwriteAction(path string, existing, newContent []byte) bool {
	hasDiff := existing != nil && newContent != nil

	for {
		action := "no"

		opts := []huh.Option[string]{
			huh.NewOption("Yes — overwrite with incoming version", "yes"),
			huh.NewOption("No  — keep my changes", "no"),
		}

		if hasDiff {
			opts = append(opts, huh.NewOption("View diff", "view"))
		}

		err := huh.NewSelect[string]().
			Title("Conflict: " + path + " has been modified since it was last generated.").
			Description("What would you like to do?").
			Options(opts...).
			Value(&action).
			Run()
		if err != nil {
			g.props.Logger.Warn(fmt.Sprintf("Prompt failed (non-interactive?): %v. Skipping overwrite.", err))

			return false
		}

		switch action {
		case "yes":
			return true
		case "no":
			return false
		case "view":
			return runDiffPager(path, existing, newContent)
		}
	}
}
