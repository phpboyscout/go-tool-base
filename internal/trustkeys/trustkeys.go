// Package trustkeys exposes the public keys embedded in the gtb binary
// for self-update signature verification (Phase 2 of the
// remote-update-checksum-verification spec).
//
// To embed a release public key, drop its ASCII-armored form into
// internal/trustkeys/keys/<name>.asc. Every *.asc file in that directory
// is embedded at build time and surfaced to the SelfUpdater via
// props.Tool.Signing.EmbeddedKeys (wired in internal/cmd/root).
//
// The directory ships with the active release public key(s) under
// keys/*.asc alongside a .gitkeep placeholder. When no *.asc file is
// present Keys returns nil, which leaves the embedded trust anchor unset
// and keeps signature verification dormant
// (setup.DefaultRequireSignature is false) until a key is added and the
// rollout flips the default. See docs/development/phase2-signing-prep.md.
package trustkeys

import (
	"embed"
	"io/fs"
	"path"

	"gitlab.com/phpboyscout/go/errors"
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
//
// A walk or read failure over the embedded filesystem is a build-time
// corruption of the binary's trust anchors, not a recoverable runtime
// condition — Keys panics rather than silently returning a partial or
// empty trust set, which would let verification fall open. Use [KeysE]
// to handle the error explicitly.
func Keys() [][]byte {
	keys, err := KeysE()
	if err != nil {
		panic("trustkeys: reading embedded trust anchors: " + err.Error())
	}

	return keys
}

// KeysE is the error-returning form of [Keys]. It propagates any error
// encountered while walking or reading the embedded keys directory rather
// than swallowing it, so trust-anchor corruption fails loud at the call
// site instead of degrading silently to an empty (verification-open) set.
func KeysE() ([][]byte, error) {
	return keysFromFS(keyFS, "keys")
}

// keysFromFS walks root in fsys, collecting every *.asc file's contents.
// It propagates the first walk or read error rather than swallowing it,
// so a corrupt trust-anchor store fails loud. Factored out of KeysE so the
// error path is unit-testable with a fault-injecting fs.FS.
func keysFromFS(fsys fs.FS, root string) ([][]byte, error) {
	var out [][]byte

	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if path.Ext(p) != ".asc" {
			return nil
		}

		b, readErr := fs.ReadFile(fsys, p)
		if readErr != nil {
			return readErr
		}

		out = append(out, b)

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "walking embedded trust-key directory")
	}

	return out, nil
}
