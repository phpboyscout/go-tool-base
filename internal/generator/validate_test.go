package generator_test

import (
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/internal/generator"
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
