package generator

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// The root and skeleton_sync honoured a rule, but the link, adapter, config
// and signing writers did not, so a deny run rewrote an ignored, edited mcp.go
// and then advised the rule that was already there.
func TestRegenerateProject_KeepsAnIgnoredFrameworkFile(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))

	const mcp = "/work/cmd/mytool/mcp.go"

	edited := editFile(t, fs, mcp)
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte("cmd/mytool/mcp.go\n"), 0o644))

	buf := regenerateAgain(t, g)
	assertContent(t, fs, mcp, edited)
	assert.False(t, buf.ContainsLevel(logger.WarnLevel, "rewrote a framework file"),
		"a kept file is not reported as rewritten; got: %v", buf.Messages())

	m, err := g.loadManifest()
	require.NoError(t, err)

	m.Properties.Features = append(m.Properties.Features, ManifestFeature{Name: "mcp", Enabled: false})
	require.NoError(t, g.writeManifest(m))

	buf = regenerateAgain(t, g)
	assertContent(t, fs, mcp, edited)
	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "so the feature stays on in the built tool"),
		"the manifest says mcp is off, but the kept file still links it; got: %v", buf.Messages())
}

// With a file the posture is written into kept by a rule, enable signing used
// to report success and record require_signature while signing.go stayed an
// empty init, so updates were never failed closed (#261).
func TestSigningPosture_RefusesAFileARuleKeeps(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	for _, tc := range []struct {
		path   string
		enable bool
	}{
		{"pkg/cmd/root/cmd.go", true},
		{"pkg/cmd/root/signing.go", true},
		{"internal/trustkeys/trustkeys.go", true},
		{"pkg/cmd/root/cmd.go", false},
		{"pkg/cmd/root/signing.go", false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			g, fs := unsignedProject(t)
			if !tc.enable {
				g, fs = signingProject(t)
			}

			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(tc.path+"\n"), 0o644))

			before := snapshotTree(t, fs)
			g.rules = nil

			var err error
			if tc.enable {
				err = g.EnableSigning(context.Background(), ManifestSigning{RequireSignature: true})
			} else {
				err = g.DisableSigning(context.Background())
			}

			require.ErrorIs(t, err, ErrSigningFileProtected)
			require.ErrorContains(t, err, tc.path)
			assert.Equal(t, before, snapshotTree(t, fs), "nothing was written")
		})
	}
}

// disable signing does not read the trust keys package, so a rule on it is no
// reason to refuse.
func TestDisableSigning_IgnoresARuleOnTheTrustKeys(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs := signingProject(t)
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte("internal/trustkeys/trustkeys.go\n"), 0o644))
	g.rules = nil

	require.NoError(t, g.DisableSigning(context.Background()))
}

// A signing change that leaves every protected file as it is writes nothing a
// rule keeps, so the rule is no reason to refuse it (#263).
func TestSigningChange_GoesAheadWhenNoKeptFileChanges(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	const rules = "pkg/cmd/root/cmd.go\npkg/cmd/root/signing.go\ninternal/trustkeys/trustkeys.go\n"

	for name, tc := range map[string]struct {
		signed bool
		change func(*Generator) error
		took   func(ManifestSigning) bool
	}{
		"disable signing that is already off": {false, func(g *Generator) error {
			return g.DisableSigning(context.Background())
		}, func(s ManifestSigning) bool { return !s.Enabled }},
		"set a field no protected file renders": {true, func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.backend", []string{"local"})
		}, func(s ManifestSigning) bool { return s.Backend == "local" }},
		"set require_signature while signing is off": {false, func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.require_signature", []string{"true"})
		}, func(s ManifestSigning) bool { return s.RequireSignature }},
	} {
		t.Run(name, func(t *testing.T) {
			g, fs := unsignedProject(t)
			if tc.signed {
				g, fs = signingProject(t)
			}

			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(rules), 0o644))
			g.rules = nil

			require.NoError(t, tc.change(g))

			m, err := g.loadManifest()
			require.NoError(t, err)
			assert.True(t, tc.took(m.Properties.Signing), "the change is recorded: %+v", m.Properties.Signing)
		})
	}
}

// The wizard asks neither the rotation overlap nor the difference between an
// empty key source and "both", so a revisit carries them rather than rewriting
// signing.go or closing the overlap (#263).
func TestCarrySigning_KeepsWhatTheWizardDoesNotAsk(t *testing.T) {
	t.Parallel()

	overlap := &ManifestSigningOverlap{KeyID: "new", PublicKey: "new.asc"}
	current := ManifestSigning{Enabled: true, KeySource: "both", Overlap: overlap, Backend: "aws-kms"}

	got := carrySigning(current, ManifestSigning{Enabled: true, Backend: "local"})
	assert.Equal(t, ManifestSigning{Enabled: true, KeySource: "both", Overlap: overlap, Backend: "local"}, got)

	assert.Equal(t, "embedded", carrySigning(current, ManifestSigning{Enabled: true, KeySource: "embedded"}).KeySource,
		"a key source the wizard chose replaces the stored one")

	off := ManifestSigning{RequireSignature: true, KeyID: "k"}
	assert.Equal(t, off, carrySigning(off, ManifestSigning{}), "a block left off stays as it was")

	assert.Equal(t, ManifestSigning{}, carrySigning(current, ManifestSigning{}),
		"turning signing off through the wizard still clears it")
}

// gtb set, gtb unset and the wizard reach the signing files through
// commitSetting, so a rule there was a second silent route into #261: the
// setting was recorded while signing.go kept its empty init.
func TestSetSetting_RefusesASigningChangeARuleBlocks(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	for name, tc := range map[string]struct {
		rule   string
		change func(*Generator) error
	}{
		"set require_signature over signing.go": {"pkg/cmd/root/signing.go", func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.require_signature", []string{"true"})
		}},
		"set key_source over signing.go": {"pkg/cmd/root/signing.go", func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.key_source", []string{"embedded"})
		}},
		"set external_key_email over signing.go": {"pkg/cmd/root/signing.go", func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.external_key_email", []string{"release@test.invalid"})
		}},
		"set require_external_crosscheck over signing.go": {"pkg/cmd/root/signing.go", func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.require_external_crosscheck", []string{"true"})
		}},
		"set require_checksum over the root": {"pkg/cmd/root/cmd.go", func(g *Generator) error {
			return g.SetSetting(context.Background(), "signing.require_checksum", []string{"true"})
		}},
		"unset enabled over signing.go": {"pkg/cmd/root/signing.go", func(g *Generator) error {
			return g.UnsetSetting(context.Background(), "signing.enabled")
		}},
	} {
		t.Run(name, func(t *testing.T) {
			g, fs := signingProject(t)
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(tc.rule+"\n"), 0o644))

			before := snapshotTree(t, fs)
			g.rules = nil

			err := tc.change(g)
			require.ErrorIs(t, err, ErrSigningFileProtected)
			require.ErrorContains(t, err, tc.rule)
			assert.Equal(t, before, snapshotTree(t, fs), "nothing was written")
		})
	}
}

// A signed project's root carries the trust keys only once it has a module
// path, so giving it one rewrites the root, whichever command sets it.
func TestSetSetting_RefusesAModulePathThatAddsTheTrustKeys(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))
	require.NoError(t, g.EnableSigning(context.Background(), ManifestSigning{}))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte("pkg/cmd/root/cmd.go\n"), 0o644))
	g.rules = nil

	before := snapshotTree(t, fs)

	err := g.SetSetting(context.Background(), "module_path", []string{"test-mod"})
	require.ErrorIs(t, err, ErrSigningFileProtected)
	assert.Equal(t, before, snapshotTree(t, fs), "nothing was written")
}

// A setting outside the signing block does not touch the signing files, so a
// rule on them is no reason to refuse it.
func TestSetSetting_IgnoresASigningRuleForAnotherSetting(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte("pkg/cmd/root/signing.go\n"), 0o644))
	g.rules = nil

	require.NoError(t, g.SetSetting(context.Background(), "config.key_case", []string{"exact"}))
}

// A rule that keeps a link file away keeps its feature out of the tool, though
// the manifest switches it on.
func TestRegenerateProject_WarnsAnIgnoredMissingLinkKeepsItsFeatureOff(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))

	require.NoError(t, fs.Remove("/work/cmd/mytool/mcp.go"))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte("cmd/mytool/mcp.go\n"), 0o644))

	buf := regenerateAgain(t, g)

	exists, _ := afero.Exists(fs, "/work/cmd/mytool/mcp.go")
	assert.False(t, exists, "the rule keeps the file away")
	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "so the feature stays off in the built tool"),
		"got: %v", buf.Messages())
}

// A rule that keeps signing.go or the root is kept, but the manifest's signing
// settings then do not ship, and the run says so (W-558).
func TestRegenerateProject_WarnsAKeptFileLacksTheSigningPosture(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	for name, tc := range map[string]struct {
		rule    string
		change  func(*ManifestSigning)
		warns   bool
		deleted bool
	}{
		"signing.go without require_signature": {"pkg/cmd/root/signing.go", func(s *ManifestSigning) { s.RequireSignature = true }, true, false},
		"signing.go kept after signing is off": {"pkg/cmd/root/signing.go", func(s *ManifestSigning) { s.Enabled = false }, true, false},
		"the root without require_checksum":    {"pkg/cmd/root/cmd.go", func(s *ManifestSigning) { s.RequireChecksum = true }, true, false},
		"the root still carrying the keys":     {"pkg/cmd/root/cmd.go", func(s *ManifestSigning) { s.Enabled = false }, true, false},
		"signing.go already matching":          {"pkg/cmd/root/signing.go", func(*ManifestSigning) {}, false, false},
		"signing.go kept away":                 {"pkg/cmd/root/signing.go", func(*ManifestSigning) {}, true, true},
		"the root already matching":            {"pkg/cmd/root/cmd.go", func(*ManifestSigning) {}, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			g, fs := signingProject(t)
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(tc.rule+"\n"), 0o644))

			if tc.deleted {
				require.NoError(t, fs.Remove("/work/"+tc.rule))
			}

			m, err := g.loadManifest()
			require.NoError(t, err)
			tc.change(&m.Properties.Signing)
			require.NoError(t, g.writeManifest(m))

			buf := regenerateAgain(t, g)

			warned := false
			for _, e := range buf.(interface{ Entries() []logger.Entry }).Entries() {
				if e.Level == logger.WarnLevel && strings.Contains(e.Message, "without the manifest's signing settings") &&
					slices.Contains(e.Keyvals, any(tc.rule)) {
					warned = true
				}
			}
			assert.Equal(t, tc.warns, warned, "got: %v", buf.Messages())
		})
	}
}

// unsignedProject is a regenerated project with a module path and signing off,
// so enabling signing really adds the trust keys to the root.
func unsignedProject(t *testing.T) (*Generator, afero.Fs) {
	t.Helper()

	g, fs, _ := newIssue13Project(t, "deny", "")
	require.NoError(t, g.RegenerateProject(context.Background()))

	m, err := g.loadManifest()
	require.NoError(t, err)
	m.Properties.ModulePath = "test-mod"
	require.NoError(t, g.writeManifest(m))

	return g, fs
}

// signingProject is unsignedProject with signing on, so the root really
// carries the trust keys a signing change could strip.
func signingProject(t *testing.T) (*Generator, afero.Fs) {
	t.Helper()

	g, fs := unsignedProject(t)
	require.NoError(t, g.EnableSigning(context.Background(), ManifestSigning{}))

	root, err := afero.ReadFile(fs, "/work/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	require.Contains(t, string(root), "EmbeddedKeys", "the fixture's root carries the trust keys")

	return g, fs
}

func snapshotTree(t *testing.T, fs afero.Fs) map[string]string {
	t.Helper()

	files := make(map[string]string)
	require.NoError(t, afero.Walk(fs, "/work", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		content, err := afero.ReadFile(fs, path)
		files[path] = string(content)

		return err
	}))

	return files
}

// regenerateAgain runs regenerate project once more on the same project with a
// fresh log.
func regenerateAgain(t *testing.T, g *Generator) logBuffer {
	t.Helper()

	buf := logger.NewBuffer()
	g.props.Logger = buf
	g.conflicts.reset()
	g.rules = nil

	require.NoError(t, g.RegenerateProject(context.Background()))

	return buf
}

func editFile(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()

	content, err := afero.ReadFile(fs, path)
	require.NoError(t, err)

	content = append(content, "// hand edit\n"...)
	require.NoError(t, afero.WriteFile(fs, path, content, 0o644))

	return content
}

func assertContent(t *testing.T, fs afero.Fs, path string, want []byte) {
	t.Helper()

	got, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}
