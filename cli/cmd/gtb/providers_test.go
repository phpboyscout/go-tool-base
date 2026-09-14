package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestEveryForgeIsLinked is the registration guard for the binary that ships
// every adapter: a profile whose provider no linked adapter registers builds
// and passes every other test, then fails the first time a user runs the
// wizard. It lived in pkg/setup/forge while the framework linked the adapters
// (spec 0194 D3 moved them here).
func TestEveryForgeIsLinked(t *testing.T) {
	t.Parallel()

	tool := props.Tool{}
	for _, d := range forge.Displays() {
		tool.Features = append(tool.Features, props.Feature{ID: d.ID, Enabled: true})
	}

	assert.Empty(t, forge.Unlinked(tool), "gtb enables every forge; providers.go must link every adapter")
}
