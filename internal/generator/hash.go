package generator

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

func calculateHash(content []byte) string {
	hash := sha256.Sum256(content)

	return hex.EncodeToString(hash[:])
}

func (g *Generator) verifyHash(path string) error {
	existingContent, err := afero.ReadFile(g.props.FS, path)
	if err != nil {
		return err
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
		g.props.Logger.Warnf("Conflict detected for %s: File has been manually modified.", path)

		confirm := g.promptOverwrite(path, nil, nil)
		if !confirm {
			g.props.Logger.Warnf("Skipping overwrite of %s", path)

			return errors.Newf("overwrite skipped by user")
		}

		g.props.Logger.Warnf("Overwriting modified file %s", path)
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

	// Default: ask — skip prompt in non-interactive environments
	if os.Getenv("GTB_NON_INTERACTIVE") == "true" {
		return false
	}

	hasDiff := existing != nil && newContent != nil

	for {
		action := "no"

		var opts []huh.Option[string]
		opts = append(opts,
			huh.NewOption("Yes — overwrite with incoming version", "yes"),
			huh.NewOption("No  — keep my changes", "no"),
		)

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
			g.props.Logger.Warnf("Prompt failed (non-interactive?): %v. Skipping overwrite.", err)

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
