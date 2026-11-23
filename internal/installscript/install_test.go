package installscript_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCurl answers the release API call with the contents of FAKE_BODY_FILE
// and FAKE_STATUS, the way curl -w '\n%{http_code}' would. The body is a file
// because a long one exceeds the kernel's limit on one environment string.
const fakeCurl = `#!/usr/bin/env bash
cat "$FAKE_BODY_FILE"
printf '\n%s' "$FAKE_STATUS"
`

// #151: an error answer from the GitLab API reached jq as a release, so the
// user saw "jq: parse error" instead of the rate limit.
func TestInstallScript_ReportsTheAPIAnswer(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}

	script, err := filepath.Abs("../../docs/install.sh")
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		status, body string
		want         []string
	}{
		"a rate limit": {
			"429", "Rate limit exceeded; see https://docs.gitlab.com/user/gitlab_com/rate_limits/",
			[]string{"HTTP 429", "Rate limit exceeded", "GITLAB_TOKEN", "read_api"},
		},
		"a refused token":             {"401", `{"message":"401 Unauthorized"}`, []string{"HTTP 401", "GITLAB_TOKEN is a valid token"}},
		"a server error":              {"502", "<html>Bad Gateway</html>\x1b]0;owned\a\x1b[2J", []string{"HTTP 502", "Bad Gateway"}},
		"a 200 that is not JSON":      {"200", "<html>maintenance</html>", []string{"was not JSON"}},
		"a release without our asset": {"200", `{"assets":{"links":[]}}`, []string{"Could not find a release asset"}},
		"an empty 200":                {"200", "", []string{"was empty, not JSON"}},
		"a 204":                       {"204", "", []string{"HTTP 204"}},
		"a rate limit with a long body": {
			"429", strings.Repeat("Rate limit exceeded.\n", 8000),
			[]string{"HTTP 429", "GITLAB_TOKEN", "read_api"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			body := filepath.Join(t.TempDir(), "body")
			require.NoError(t, os.WriteFile(body, []byte(tc.body), 0o600))

			bin := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(bin, "curl"), []byte(fakeCurl), 0o755))

			cmd := exec.Command("bash", script)
			cmd.Dir = t.TempDir()
			cmd.Env = []string{
				"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
				"HOME=" + t.TempDir(),
				"GTB_NON_INTERACTIVE=true",
				"FAKE_STATUS=" + tc.status,
				"FAKE_BODY_FILE=" + body,
			}

			out, err := cmd.CombinedOutput()
			require.Error(t, err, "the install stops")

			for _, w := range tc.want {
				assert.Contains(t, string(out), w)
			}

			assert.NotContains(t, string(out), "jq: parse error")
			for i, c := range out {
				if (c < 0x20 && c != '\t' && c != '\n') || c >= 0x7f {
					assert.Failf(t, "the server's answer cannot drive the terminal", "byte 0x%02x at offset %d", c, i)

					break
				}
			}
		})
	}
}
