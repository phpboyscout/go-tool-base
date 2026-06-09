// Package trustkeys exposes the public keys embedded in the gtb binary
// for self-update signature verification (Phase 2 of the
// remote-update-checksum-verification spec).
//
// To embed a release public key, drop its ASCII-armored form into
// internal/trustkeys/keys/<name>.asc. Every *.asc file in that directory
// is embedded at build time and surfaced to the SelfUpdater via
// props.Tool.Signing.EmbeddedKeys (wired in internal/cmd/root).
//
// The directory ships empty (only a .gitkeep): with no keys present
// Keys returns nil, so signature verification stays dormant
// (setup.DefaultRequireSignature is false) until a real key is added and
// the rollout flips the default. See docs/development/phase2-signing-prep.md.
package trustkeys

import (
	"embed"
	"io/fs"
	"path"
)

// keyFS embeds the keys directory. `all:` is required so the directory
// matches even when it contains only a dotfile (.gitkeep); without an
// embeddable entry the //go:embed directive would fail to compile.
//
//go:embed all:keys
var keyFS embed.FS

// Keys returns every embedded ASCII-armored public key (the contents of
// internal/trustkeys/keys/*.asc). It returns nil when no key files are
// present, which leaves the embedded trust anchor unset.
func Keys() [][]byte {
	var out [][]byte

	_ = fs.WalkDir(keyFS, "keys", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		if path.Ext(p) != ".asc" {
			return nil
		}

		b, readErr := keyFS.ReadFile(p)
		if readErr != nil {
			return readErr
		}

		out = append(out, b)

		return nil
	})

	return out
}
