package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestCheckCredentialResolution_ReportsOnlyEnabledFeatures pins #55: the
// resolution check reports the credentials of enabled features, not every
// credential the framework knows how to declare. A tool with no ai and no
// forge used to report "5 credential(s) resolve".
func TestCheckCredentialResolution_ReportsOnlyEnabledFeatures(t *testing.T) {
	// The check reports a credential only when it resolves, so each one
	// points at a variable this test sets rather than at whatever the
	// developer's shell happens to export.
	t.Setenv("NARROW_TEST_GITHUB_TOKEN", "x")
	t.Setenv("NARROW_TEST_GITLAB_TOKEN", "x")
	t.Setenv("NARROW_TEST_ANTHROPIC_KEY", "x")

	cfg := "github:\n  auth:\n    env: NARROW_TEST_GITHUB_TOKEN\ngitlab:\n  auth:\n    env: NARROW_TEST_GITLAB_TOKEN\nanthropic:\n  api:\n    env: NARROW_TEST_ANTHROPIC_KEY\n"

	narrow := &p.Props{Tool: p.Tool{Name: "t"}, Config: testutil.StoreFromYAML(t, cfg)}
	res := checkCredentialResolution(context.Background(), narrow)
	assert.Equal(t, CheckSkip, res.Status, res.Message)
	assert.Contains(t, res.Message, "no credentials", "nothing enabled consumes a credential")

	gitlabOnly := &p.Props{
		Tool:   p.Tool{Name: "t", Features: p.SetFeatures(p.Enable(forge.GitlabFeature))},
		Config: testutil.StoreFromYAML(t, cfg),
	}
	res = checkCredentialResolution(context.Background(), gitlabOnly)
	require.NotEqual(t, CheckSkip, res.Status, res.Message)
	assert.Contains(t, res.Details, "GitLab")
	assert.NotContains(t, res.Details, "GitHub", "a forge that is not enabled is not reported")
	assert.NotContains(t, res.Details, "Anthropic", "ai is not enabled")
}
