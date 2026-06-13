package generator_test

import (
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
)

func TestValidateName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple lowercase", input: "mytool", wantErr: false},
		{name: "with hyphen", input: "my-tool", wantErr: false},
		{name: "single char", input: "a", wantErr: false},
		{name: "max length", input: "a" + strings.Repeat("b", 63), wantErr: false},

		{name: "empty", input: "", wantErr: true},
		{name: "uppercase rejected", input: "MyTool", wantErr: true},
		{name: "underscore rejected", input: "my_tool", wantErr: true},
		{name: "leading digit", input: "123tool", wantErr: true},
		{name: "leading hyphen", input: "-tool", wantErr: true},
		{name: "too long", input: "a" + strings.Repeat("b", 64), wantErr: true},
		{name: "traversal", input: "../tool", wantErr: true},
		{name: "newline", input: "tool\n", wantErr: true},
		{name: "NUL byte", input: "tool\x00", wantErr: true},
		{name: "cyrillic homoglyph", input: "myтool", wantErr: true},
		{name: "right-to-left override", input: "my\u202etool", wantErr: true},
		{name: "zero-width joiner", input: "my\u200dtool", wantErr: true},
		{name: "BOM", input: "\ufeffmytool", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateName(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple prose", input: "A friendly tool", wantErr: false},
		{name: "punctuation", input: "A tool, version 1.0.0.", wantErr: false},
		{name: "empty accepted", input: "", wantErr: false},
		{name: "500 bytes exactly", input: strings.Repeat("a", 500), wantErr: false},
		{name: "unicode prose", input: "A café tool", wantErr: false},
		{name: "tab is allowed", input: "Two\tcolumns", wantErr: false},

		{name: "501 bytes rejected", input: strings.Repeat("a", 501), wantErr: true},
		{name: "template-open", input: "uses {{ .Foo }}", wantErr: true},
		{name: "template-close", input: "bad }}", wantErr: true},
		{name: "newline control", input: "line1\nline2", wantErr: true},
		{name: "NUL byte", input: "\x00", wantErr: true},
		{name: "DEL control", input: "bad\x7f", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateDescription(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "github simple", input: "github.com/org/repo", wantErr: false},
		{name: "gitlab deep", input: "gitlab.com/group/sub/repo", wantErr: false},
		{name: "with underscore", input: "example.com/user/my_tool", wantErr: false},
		{name: "with dots", input: "example.com/org/repo.v2", wantErr: false},

		{name: "empty", input: "", wantErr: true},
		{name: "traversal", input: "../repo", wantErr: true},
		{name: "leading slash", input: "/github.com/org/repo", wantErr: true},
		{name: "trailing slash", input: "github.com/org/repo/", wantErr: true},
		{name: "single segment", input: "github.com", wantErr: true},
		{name: "scheme prefix", input: "https://github.com/org/repo", wantErr: true},
		{name: "empty segment", input: "github.com//repo", wantErr: true},
		{name: "dot segment", input: "github.com/./repo", wantErr: true},
		{name: "contains space", input: "github.com/org name/repo", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateRepo(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "github", input: "github.com", wantErr: false},
		{name: "nested subdomain", input: "gitlab.example.com", wantErr: false},
		{name: "host with port", input: "localhost:8080", wantErr: false},
		{name: "punycode", input: "xn--nxasmq6b.example.com", wantErr: false},

		{name: "empty", input: "", wantErr: true},
		{name: "raw unicode", input: "\u4f8b\u3048.jp", wantErr: true},
		{name: "cyrillic homoglyph", input: "githuВ.com", wantErr: true},
		{name: "port empty", input: "github.com:", wantErr: true},
		{name: "non-numeric port", input: "github.com:abc", wantErr: true},
		{name: "space", input: "github com", wantErr: true},
		{name: "control", input: "github.com\n", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateHost(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateOrg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           string
		releaseProvider string
		wantErr         bool
	}{
		{name: "github simple", input: "myorg", releaseProvider: "github", wantErr: false},
		{name: "github mixed case", input: "MyOrg", releaseProvider: "github", wantErr: false},
		{name: "github 39 chars", input: "a" + strings.Repeat("b", 38), releaseProvider: "github", wantErr: false},
		{name: "default to github", input: "myorg", releaseProvider: "", wantErr: false},

		{name: "github empty", input: "", releaseProvider: "github", wantErr: true},
		{name: "github leading hyphen", input: "-myorg", releaseProvider: "github", wantErr: true},
		{name: "github 40 chars", input: "a" + strings.Repeat("b", 39), releaseProvider: "github", wantErr: true},
		{name: "github slash", input: "my/org", releaseProvider: "github", wantErr: true},
		{name: "github unicode", input: "myорг", releaseProvider: "github", wantErr: true},

		{name: "gitlab single", input: "group", releaseProvider: "gitlab", wantErr: false},
		{name: "gitlab 3-deep", input: "group/sub/subsub", releaseProvider: "gitlab", wantErr: false},
		{name: "gitlab 4-deep", input: "a/b/c/d", releaseProvider: "gitlab", wantErr: false},

		{name: "gitlab 5-deep rejected", input: "a/b/c/d/e", releaseProvider: "gitlab", wantErr: true},
		{name: "gitlab bad segment", input: "group/-bad", releaseProvider: "gitlab", wantErr: true},

		{name: "unknown provider", input: "myorg", releaseProvider: "bitbucket", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateOrg(tc.input, tc.releaseProvider)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateEnvPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "simple", input: "GTB", wantErr: false},
		{name: "with underscore and digits", input: "MY_TOOL_V2", wantErr: false},
		{name: "32 chars max", input: "A" + strings.Repeat("B", 31), wantErr: false},

		{name: "lowercase rejected", input: "gtb", wantErr: true},
		{name: "leading digit", input: "1TOOL", wantErr: true},
		{name: "leading underscore", input: "_TOOL", wantErr: true},
		{name: "33 chars", input: "A" + strings.Repeat("B", 32), wantErr: true},
		{name: "shell meta", input: "MY-TOOL", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateEnvPrefix(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSlackChannel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "simple", input: "help", wantErr: false},
		{name: "with hyphen and digits", input: "my-team-v2-help", wantErr: false},
		{name: "leading hash stripped", input: "#help", wantErr: false},
		{name: "80 chars max", input: strings.Repeat("a", 80), wantErr: false},

		{name: "uppercase rejected", input: "Help", wantErr: true},
		{name: "underscore rejected", input: "team_help", wantErr: true},
		{name: "81 chars", input: strings.Repeat("a", 81), wantErr: true},
		{name: "bang", input: "team!help", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateSlackChannel(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateTelemetryEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "https", input: "https://telemetry.corp.example/events", wantErr: false},
		{name: "http", input: "http://localhost:4317/v1/events", wantErr: false},

		{name: "ftp rejected", input: "ftp://mirror.example/events", wantErr: true},
		{name: "file rejected", input: "file:///etc/passwd", wantErr: true},
		{name: "no host", input: "https://", wantErr: true},
		{name: "control char", input: "https://telemetry.corp.example/\x00", wantErr: true},
		{name: "too long", input: "https://example.com/" + strings.Repeat("a", 2100), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateTelemetryEndpoint(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidate_HintContainsFieldAndRule(t *testing.T) {
	t.Parallel()

	err := generator.ValidateName("BadName")
	require.Error(t, err)

	hint := errors.FlattenHints(err)
	require.Contains(t, hint, "Name")
	require.Contains(t, hint, "lowercase")
}

func TestValidateCommandName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple lowercase", input: "deploy", wantErr: false},
		{name: "kebab", input: "create-user", wantErr: false},
		{name: "underscore allowed", input: "create_user", wantErr: false},
		{name: "kebab and underscore mixed", input: "my-cmd_v2", wantErr: false},
		{name: "single char", input: "a", wantErr: false},
		{name: "digits after first", input: "cmd2", wantErr: false},
		{name: "max length 64", input: "a" + strings.Repeat("b", 63), wantErr: false},

		{name: "empty", input: "", wantErr: true},
		{name: "reserved options", input: "options", wantErr: true},
		{name: "reserved root", input: "root", wantErr: true},
		{name: "dot", input: ".", wantErr: true},
		{name: "dot dot", input: "..", wantErr: true},
		{name: "embedded dot", input: "cmd.go", wantErr: true},
		{name: "slash", input: "a/b", wantErr: true},
		{name: "backslash", input: `a\b`, wantErr: true},
		{name: "traversal", input: "../../evil", wantErr: true},
		{name: "leading digit", input: "2cmd", wantErr: true},
		{name: "leading hyphen", input: "-cmd", wantErr: true},
		{name: "leading underscore", input: "_cmd", wantErr: true},
		{name: "uppercase", input: "Deploy", wantErr: true},
		{name: "space", input: "my cmd", wantErr: true},
		{name: "over-length 65", input: "a" + strings.Repeat("b", 64), wantErr: true},
		{name: "newline", input: "cmd\n", wantErr: true},
		{name: "NUL byte", input: "cmd\x00", wantErr: true},
		{name: "cyrillic homoglyph", input: "dеploy", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateCommandName(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateParentPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty means root", input: "", wantErr: false},
		{name: "root literal", input: "root", wantErr: false},
		{name: "single parent", input: "kube", wantErr: false},
		{name: "nested parent", input: "kube/ctx", wantErr: false},
		{name: "surrounding slashes trimmed", input: "/kube/ctx/", wantErr: false},

		{name: "traversal segment", input: "../evil", wantErr: true},
		{name: "nested traversal", input: "kube/../../evil", wantErr: true},
		{name: "dot segment", input: "kube/./ctx", wantErr: true},
		{name: "uppercase segment", input: "Kube/ctx", wantErr: true},
		{name: "reserved options segment", input: "options", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateParentPath(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSigningBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "aws-kms", input: "aws-kms", wantErr: false},
		{name: "local", input: "local", wantErr: false},

		{name: "uppercase", input: "AWS-KMS", wantErr: true},
		{name: "leading digit", input: "1kms", wantErr: true},
		{name: "yaml breakout", input: "x\"\n  - artifact: pwned", wantErr: true},
		{name: "space", input: "aws kms", wantErr: true},
		{name: "over-length", input: strings.Repeat("a", 33), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateSigningBackend(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSigningKMSRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "eu-west-2", input: "eu-west-2", wantErr: false},
		{name: "us-gov-west-1", input: "us-gov-west-1", wantErr: false},

		{name: "uppercase", input: "EU-WEST-2", wantErr: true},
		{name: "underscore", input: "eu_west_2", wantErr: true},
		{name: "yaml breakout", input: "x\"\n  - artifact: pwned", wantErr: true},
		{name: "over-length", input: strings.Repeat("a", 33), wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateSigningKMSRegion(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSigningKeyID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "kms alias", input: "alias/acme-release-signing-v1", wantErr: false},
		{name: "kms uuid", input: "1234abcd-12ab-34cd-56ef-1234567890ab", wantErr: false},
		{name: "kms arn", input: "arn:aws:kms:eu-west-2:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", wantErr: false},
		{name: "local pem path", input: "./release.pem", wantErr: false},

		{name: "yaml injection", input: "x\"\n  - artifact: pwned", wantErr: true},
		{name: "newline", input: "alias/x\nalias/y", wantErr: true},
		{name: "double quote", input: `alias/"x"`, wantErr: true},
		{name: "space", input: "alias/x y", wantErr: true},
		{name: "NUL byte", input: "alias/x\x00", wantErr: true},
		{name: "over-length", input: strings.Repeat("a", 257), wantErr: true},

		{name: "dotdot traversal", input: "../../etc/x", wantErr: true},
		{name: "dotdot in alias", input: "alias/..", wantErr: true},
		{name: "dotdot interior", input: "../release.pem", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateSigningKeyID(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSigningPublicKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "empty accepted", input: "", wantErr: false},
		{name: "default convention", input: "internal/trustkeys/keys/signing-key-v1.asc", wantErr: false},
		{name: "single segment", input: "key.asc", wantErr: false},
		{name: "leading dot-slash normalised", input: "./key.asc", wantErr: false},
		{name: "leading dot-slash with subdir", input: "./sub/key.asc", wantErr: false},

		{name: "absolute path", input: "/etc/passwd", wantErr: true},
		{name: "traversal", input: "../../home/user/.ssh/id_rsa", wantErr: true},
		{name: "interior traversal", input: "keys/../../escape.asc", wantErr: true},
		{name: "dot-slash traversal escape", input: "./../escape.asc", wantErr: true},
		{name: "absolute key", input: "/abs/key.asc", wantErr: true},
		{name: "parent escape", input: "../escape.asc", wantErr: true},
		{name: "doubled dot-slash", input: "././key.asc", wantErr: true},
		{name: "backslash separator", input: `keys\key.asc`, wantErr: true},
		{name: "yaml injection", input: "x\"\n  - artifact: pwned", wantErr: true},
		{name: "inline armor rejected", input: "-----BEGIN PGP PUBLIC KEY BLOCK-----", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := generator.ValidateSigningPublicKey(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				require.ErrorIs(t, err, generator.ErrInvalidInput)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// validTestManifest returns a minimal manifest that passes ValidateManifest,
// for the walk tests to mutate.
func validTestManifest() *generator.Manifest {
	m := &generator.Manifest{}
	m.Properties.Name = "mytool"

	return m
}

// TestValidateManifest_WalksCommands proves ValidateManifest recursively
// validates every command name in the manifest tree, so the regenerate path
// (which trusts the manifest) is covered against path traversal.
func TestValidateManifest_WalksCommands(t *testing.T) {
	t.Parallel()

	t.Run("valid nested commands accepted", func(t *testing.T) {
		t.Parallel()

		m := validTestManifest()
		m.Commands = []generator.ManifestCommand{
			{Name: "deploy", Commands: []generator.ManifestCommand{{Name: "status_check"}}},
		}
		require.NoError(t, generator.ValidateManifest(m))
	})

	t.Run("traversal in top-level command rejected", func(t *testing.T) {
		t.Parallel()

		m := validTestManifest()
		m.Commands = []generator.ManifestCommand{{Name: "../../evil"}}

		err := generator.ValidateManifest(m)
		require.Error(t, err)
		require.ErrorIs(t, err, generator.ErrInvalidInput)
	})

	t.Run("traversal in nested command rejected", func(t *testing.T) {
		t.Parallel()

		m := validTestManifest()
		m.Commands = []generator.ManifestCommand{
			{Name: "good", Commands: []generator.ManifestCommand{{Name: "../escape"}}},
		}

		err := generator.ValidateManifest(m)
		require.Error(t, err)
		require.ErrorIs(t, err, generator.ErrInvalidInput)
	})
}

// TestValidateManifest_WalksSigning proves ValidateManifest validates every
// ManifestSigning field that is rendered into the CI-executed
// .goreleaser.yaml.
func TestValidateManifest_WalksSigning(t *testing.T) {
	t.Parallel()

	t.Run("valid signing block accepted", func(t *testing.T) {
		t.Parallel()

		m := validTestManifest()
		m.Properties.Signing = generator.ManifestSigning{
			Enabled:   true,
			Backend:   "aws-kms",
			KMSRegion: "eu-west-2",
			KeyID:     "alias/acme-release-signing-v1",
			PublicKey: "internal/trustkeys/keys/signing-key-v1.asc",
		}
		require.NoError(t, generator.ValidateManifest(m))
	})

	tests := []struct {
		name   string
		mutate func(*generator.ManifestSigning)
	}{
		{name: "backend injection", mutate: func(s *generator.ManifestSigning) { s.Backend = "x\"\n  - artifact: pwned" }},
		{name: "region injection", mutate: func(s *generator.ManifestSigning) { s.KMSRegion = "x\"\n  - artifact: pwned" }},
		{name: "key id injection", mutate: func(s *generator.ManifestSigning) { s.KeyID = "x\"\n  - artifact: pwned" }},
		{name: "public key traversal", mutate: func(s *generator.ManifestSigning) { s.PublicKey = "../../etc/passwd" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := validTestManifest()
			m.Properties.Signing = generator.ManifestSigning{Enabled: true}
			tc.mutate(&m.Properties.Signing)

			err := generator.ValidateManifest(m)
			require.Error(t, err)
			require.ErrorIs(t, err, generator.ErrInvalidInput)
		})
	}
}
