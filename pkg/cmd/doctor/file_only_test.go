package doctor

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0214 D10: doctor lists the file-only patterns and names any variable
// the root ignored on this run, never its value.
func TestCheckFileOnlyKeys(t *testing.T) {
	t.Parallel()

	t.Run("nothing declared skips", func(t *testing.T) {
		t.Parallel()

		res := checkFileOnlyKeys(t.Context(), &p.Props{Tool: p.Tool{Name: "t"}})

		assert.Equal(t, "File-only config keys", res.Name)
		assert.Equal(t, CheckSkip, res.Status)
	})

	t.Run("declared patterns are listed", func(t *testing.T) {
		t.Parallel()

		res := checkFileOnlyKeys(t.Context(), &p.Props{Tool: p.Tool{
			Name:      "t",
			EnvPrefix: "T",
			Config:    p.ConfigSpec{FileOnly: []string{"update.**", "config.sources.**"}},
		}})

		assert.Equal(t, CheckPass, res.Status)
		assert.Equal(t, "no T_ variable sets update.**, config.sources.**", res.Message,
			"it promises only the prefixed variables the filter covers")
	})

	t.Run("without a prefix no variable reaches the store", func(t *testing.T) {
		t.Parallel()

		res := checkFileOnlyKeys(t.Context(), &p.Props{Tool: p.Tool{
			Name:   "t",
			Config: p.ConfigSpec{FileOnly: []string{"update.**"}},
		}})

		assert.Equal(t, "no config-layer variable sets update.**", res.Message)
	})

	t.Run("a withheld variable warns by name", func(t *testing.T) {
		t.Parallel()

		res := checkFileOnlyKeys(t.Context(), &p.Props{
			Tool:        p.Tool{Name: "t", Config: p.ConfigSpec{FileOnly: []string{"update.**"}}},
			WithheldEnv: []string{"T_UPDATE", "T_UPDATE_REQUIRE_SIGNATURE"},
		})

		assert.Equal(t, CheckWarn, res.Status)
		assert.Contains(t, res.Message, "update.**")
		assert.Contains(t, res.Details, "T_UPDATE")
		assert.Contains(t, res.Details, "T_UPDATE_REQUIRE_SIGNATURE")
	})
}

func TestDefaultChecks_IncludeFileOnlyKeys(t *testing.T) {
	t.Parallel()

	want := reflect.ValueOf(checkFileOnlyKeys).Pointer()

	assert.True(t, slices.ContainsFunc(DefaultChecks(nil), func(c CheckFunc) bool {
		return reflect.ValueOf(c).Pointer() == want
	}))
}
