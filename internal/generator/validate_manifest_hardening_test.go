package generator_test

// Regression tests for the manifest-validation hardening spec
// (docs/development/specs/2026-07-23-generator-manifest-validation-hardening.md).
// Each test drives a hostile value through ValidateManifest — the gate the
// regenerate and manifest-update paths rely on — and requires rejection
// before any file write. On the pre-hardening generator every hostile case
// here passed the gate.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
)

func TestValidateFlagDefaultCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: false},
		{name: "identifier", input: "defaultTimeout", wantErr: false},
		{name: "underscore start", input: "_private", wantErr: false},
		{name: "selector", input: "time.Second", wantErr: false},
		{name: "max length", input: strings.Repeat("a", 128), wantErr: false},

		{name: "too long", input: strings.Repeat("a", 129), wantErr: true},
		{name: "leading digit", input: "5timeout", wantErr: true},
		{name: "space", input: "time. Second", wantErr: true},
		{name: "operator", input: "a+b", wantErr: true},
		{name: "trailing dot", input: "time.", wantErr: true},
		{name: "leading dot", input: ".Second", wantErr: true},
		{name: "double dot", input: "time..Second", wantErr: true},
		{name: "paren", input: "f()", wantErr: true},
		{name: "unicode identifier rejected", input: "tïme.Second", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateFlagDefaultCode(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateLongDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: false},
		{name: "multi-line", input: "line1\nline2\n\nline4", wantErr: false},
		{name: "tabs", input: "col1\tcol2", wantErr: false},
		{name: "pipes allowed (sink escapes)", input: "a | b", wantErr: false},
		{name: "max length", input: strings.Repeat("a", 4000), wantErr: false},

		{name: "too long", input: strings.Repeat("a", 4001), wantErr: true},
		{name: "NUL", input: "a\x00b", wantErr: true},
		{name: "escape char", input: "a\x1bb", wantErr: true},
		{name: "carriage return alone", input: "a\rb", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateLongDescription(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRepoName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple", input: "my-tool", wantErr: false},
		{name: "dots tilde underscore", input: "a1._~-x", wantErr: false},

		{name: "empty", input: "", wantErr: true},
		{name: "slash", input: "org/repo", wantErr: true},
		{name: "quote", input: `re"po`, wantErr: true},
		{name: "too long", input: strings.Repeat("a", 256), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateRepoName(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSigningExternalKeyEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty", input: "", wantErr: false},
		{name: "plain", input: "release@example.com", wantErr: false},
		{name: "plus tag", input: "release+signing@example.co.uk", wantErr: false},

		{name: "double at", input: "a@b@example.com", wantErr: true},
		{name: "trailing dot domain", input: "a@example.", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 250) + "@b.com", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateSigningExternalKeyEmail(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// baseManifest returns a minimal valid manifest the hostile cases mutate.
func baseManifest() *generator.Manifest {
	return &generator.Manifest{
		Properties: generator.ManifestProperties{
			Name: "mytool",
		},
	}
}

func TestValidateManifestReleaseSourceRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		repo    string
		wantErr bool
	}{
		{name: "empty (optional)", repo: "", wantErr: false},
		{name: "plain repo name", repo: "my-tool", wantErr: false},
		{name: "dots and underscores", repo: "my_tool.v2", wantErr: false},

		{name: "quote breakout into CI JSON list", repo: `x", "hostile/repo`, wantErr: true},
		{name: "newline", repo: "repo\nhostile", wantErr: true},
		{name: "whitespace", repo: "repo name", wantErr: true},
		{name: "bracket", repo: "repo[0]", wantErr: true},
		{name: "traversal segment", repo: "..", wantErr: true},
		{name: "leading dot", repo: ".hidden", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := baseManifest()
			m.ReleaseSource.Repo = tc.repo

			err := generator.ValidateManifest(m)
			if tc.wantErr {
				require.Error(t, err, "hostile release_source.repo %q must not pass ValidateManifest", tc.repo)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateManifestReleaseSourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		typ     string
		wantErr bool
	}{
		{name: "empty defaults", typ: "", wantErr: false},
		{name: "github", typ: "github", wantErr: false},
		{name: "gitlab", typ: "gitlab", wantErr: false},

		// Reserved until skeleton assets exist for them.
		{name: "gitea reserved", typ: "gitea", wantErr: true},
		{name: "bitbucket reserved", typ: "bitbucket", wantErr: true},
		{name: "arbitrary value", typ: "evil\ninjection", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := baseManifest()
			m.ReleaseSource.Type = tc.typ

			err := generator.ValidateManifest(m)
			if tc.wantErr {
				require.Error(t, err, "release_source.type %q must not pass ValidateManifest", tc.typ)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateManifestFlagDefaultCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		def     string
		wantErr bool
	}{
		{name: "empty (zero value)", def: "", wantErr: false},
		{name: "bare identifier", def: "defaultTimeout", wantErr: false},
		{name: "qualified identifier", def: "time.Second", wantErr: false},
		{name: "deep selector", def: "pkg.sub.Const", wantErr: false},

		{name: "function call", def: `os.Getenv("PWNED")`, wantErr: true},
		{name: "statement injection", def: "0; func init() { panic(1) }", wantErr: true},
		{name: "expression", def: "5 * time.Second", wantErr: true},
		{name: "composite literal", def: "[]string{x}", wantErr: true},
		{name: "newline", def: "x\ny", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := baseManifest()
			m.Commands = []generator.ManifestCommand{{
				Name:        "deploy",
				Description: "Deploy the thing",
				Flags: []generator.ManifestFlag{{
					Name:          "timeout",
					Type:          "duration",
					Description:   "Timeout",
					Default:       tc.def,
					DefaultIsCode: true,
				}},
			}}

			err := generator.ValidateManifest(m)
			if tc.wantErr {
				require.Error(t, err, "code default %q must not pass ValidateManifest", tc.def)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateManifestFlagFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flag    generator.ManifestFlag
		wantErr bool
	}{
		{
			name:    "valid flag",
			flag:    generator.ManifestFlag{Name: "region", Type: "string", Description: "Region"},
			wantErr: false,
		},
		{
			name:    "hostile flag name",
			flag:    generator.ManifestFlag{Name: "Bad Flag!", Type: "string"},
			wantErr: true,
		},
		{
			name:    "unknown flag type",
			flag:    generator.ManifestFlag{Name: "region", Type: "chan int"},
			wantErr: true,
		},
		{
			name:    "multi-rune shorthand",
			flag:    generator.ManifestFlag{Name: "region", Type: "string", Shorthand: "-r; rm -rf"},
			wantErr: true,
		},
		{
			name:    "template-lookalike description",
			flag:    generator.ManifestFlag{Name: "region", Type: "string", Description: "{{ .Hostile }}"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := baseManifest()
			m.Commands = []generator.ManifestCommand{{
				Name:  "deploy",
				Flags: []generator.ManifestFlag{tc.flag},
			}}

			err := generator.ValidateManifest(m)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateManifestCommandDescriptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cmd     generator.ManifestCommand
		wantErr bool
	}{
		{
			name: "valid descriptions incl multi-line long",
			cmd: generator.ManifestCommand{
				Name:            "deploy",
				Description:     "Deploy the thing",
				LongDescription: "Deploy the thing.\n\nWith detail over\nseveral lines.",
			},
			wantErr: false,
		},
		{
			name: "template-lookalike description",
			cmd: generator.ManifestCommand{
				Name:        "deploy",
				Description: "{{ .Hostile }}",
			},
			wantErr: true,
		},
		{
			name: "control character in long description",
			cmd: generator.ManifestCommand{
				Name:            "deploy",
				LongDescription: "long\x00desc",
			},
			wantErr: true,
		},
		{
			name: "nested command validated too",
			cmd: generator.ManifestCommand{
				Name: "deploy",
				Commands: []generator.ManifestCommand{{
					Name:        "now",
					Description: "bell\x07char",
				}},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := baseManifest()
			m.Commands = []generator.ManifestCommand{tc.cmd}

			err := generator.ValidateManifest(m)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateManifestSigningProvenanceFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		email     string
		keySource string
		wantErr   bool
	}{
		{name: "empty optional fields", email: "", keySource: "", wantErr: false},
		{name: "valid email and source", email: "release@example.com", keySource: "embedded", wantErr: false},
		{name: "external", email: "release@example.com", keySource: "external", wantErr: false},
		{name: "both", email: "release@example.com", keySource: "both", wantErr: false},

		{name: "newline breaks provenance comment", email: "a@b.com\n// injected", keySource: "", wantErr: true},
		{name: "whitespace in email", email: "a b@example.com", keySource: "", wantErr: true},
		{name: "no at sign", email: "not-an-email", keySource: "", wantErr: true},
		{name: "out-of-enum key source", email: "", keySource: "hostile\nstuff", wantErr: true},
		{name: "unknown key source", email: "", keySource: "network", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := baseManifest()
			m.Properties.Signing.Enabled = true
			m.Properties.Signing.ExternalKeyEmail = tc.email
			m.Properties.Signing.KeySource = tc.keySource

			err := generator.ValidateManifest(m)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
