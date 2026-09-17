package docs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestMissingAssetsHint(t *testing.T) {
	t.Parallel()

	t.Run("never ships the gtb curl|bash install line", func(t *testing.T) {
		t.Parallel()

		hint := missingAssetsHint(props.Tool{Name: "mytool"})
		assert.NotContains(t, hint, "githubusercontent.com")
		assert.NotContains(t, hint, "curl -sSL")
		assert.Contains(t, hint, "mytool")
	})

	t.Run("names what is missing and does not blame go install for a release build (#74)", func(t *testing.T) {
		t.Parallel()

		hint := missingAssetsHint(props.Tool{Name: "mytool"})
		assert.Contains(t, hint, "assets/docs", "says what is missing")
		assert.Contains(t, hint, "release build", "says where a build gets it")
		assert.NotContains(t, hint, "It looks like you might have installed using 'go install'")
	})

	t.Run("uses the tool-supplied install hint when set", func(t *testing.T) {
		t.Parallel()

		hint := missingAssetsHint(props.Tool{Name: "mytool", InstallHint: "brew install mytool"})
		assert.True(t, strings.HasSuffix(hint, "\nbrew install mytool"), hint)
		assert.NotContains(t, hint, "recommended installation method")
	})

	t.Run("falls back gracefully without a tool name", func(t *testing.T) {
		t.Parallel()

		hint := missingAssetsHint(props.Tool{})
		assert.NotEmpty(t, strings.TrimSpace(hint))
		assert.NotContains(t, hint, "githubusercontent.com")
	})
}
