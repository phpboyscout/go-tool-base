package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestRegisterRootCommandsOn: a feature contributes top-level commands under
// its own slot, which the root reads by feature; nil providers contribute
// nothing and calls append (spec 0202 D2).
func TestRegisterRootCommandsOn(t *testing.T) {
	t.Parallel()

	const (
		a = props.FeatureID("root-a")
		b = props.FeatureID("root-b")
	)

	r := features.NewRegistry()
	provider := setup.RootCommandProvider(func(*props.Props) *setup.Command { return nil })

	setup.RegisterRootCommandsOn(r, a, provider)
	setup.RegisterRootCommandsOn(r, a, provider, nil)
	setup.RegisterRootCommandsOn(r, b)

	got := setup.RootCommandsIn(r.Snapshot())
	require.Len(t, got[a], 2, "two calls append; a nil provider is skipped")
	assert.Empty(t, got[b], "a call with no providers contributes nothing")

	assert.Empty(t, setup.SubcommandsIn(r.Snapshot())[a], "a root-command provider is not an init subcommand")
}
