package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
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

func (g *Generator) verifyHash(path string) error {
	existingContent, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return errors.Wrap(err, "failed to read file for hash verification")
	}

	currentHash := calculateHash(existingContent)

	// Retrieve stored hash from manifest if available
	var storedHash string

	if cmd, err := g.findManifestCommand(); err == nil && cmd != nil {
		filename := filepath.Base(path)

		storedHash = cmd.Hashes[filename]
		if storedHash == "" && filename == "cmd.go" {
			storedHash = cmd.Hash
		}
	}

	// If hashes differ and we are not forcing, prompt the user
	if storedHash != "" && storedHash != currentHash && !g.config.Force {
		g.props.Logger.Warn("conflict detected: file has been manually modified", "path", path,
			"hint", ignoreConflictHint(path))

		confirm := g.promptOverwrite(path, nil, nil)
		if !confirm {
			g.props.Logger.Warn("skipping overwrite", "path", path)

			return errors.Newf("overwrite skipped by user")
		}

		g.props.Logger.Warn("overwriting modified file", "path", path)
	}

	return nil
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
	if isNonInteractive() {
		return false
	}

	return g.askOverwriteAction(path, existing, newContent)
}

// isNonInteractive reports whether the generator is running without a usable
// controlling terminal, so interactive huh prompts must be skipped rather than
// attempted. It honours the explicit GTB_NON_INTERACTIVE=true opt-out, the
// CI=true signal the rest of the toolchain treats as non-interactive
// (pkg/cmd/root.isCIEnvironment), and finally the absence of a TTY on stdin.
func isNonInteractive() bool {
	if os.Getenv("GTB_NON_INTERACTIVE") == "true" {
		return true
	}

	if os.Getenv("CI") == "true" {
		return true
	}

	if !utils.IsInteractive() {
		return true
	}

	// stdin looks like a terminal, but huh/bubbletea drives the controlling
	// terminal (/dev/tty on unix, the console on Windows), not stdin. In some
	// headless containers a char-device stdin (e.g. /dev/null) coexists with no
	// attachable controlling terminal, so probe it directly and skip cleanly
	// rather than letting huh fail with an "open /dev/tty" error (issue #6.2).
	return !controllingTerminalAvailable()
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
