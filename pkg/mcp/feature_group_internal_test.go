package mcp

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestFeatureGroup(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "tool"}

	wrapped := setup.Wrap(props.ConfigCmd, &cobra.Command{Use: "get"})
	topLevel := &cobra.Command{Use: "post"}
	parent := &cobra.Command{Use: "deploy"}
	nested := &cobra.Command{Use: "status"}

	parent.AddCommand(nested)
	root.AddCommand(wrapped.Command, topLevel, parent)

	assert.Equal(t, string(props.ConfigCmd), featureGroup(wrapped.Command), "a wrapped command is grouped by its feature")
	assert.Equal(t, "deploy", featureGroup(nested), "an unwrapped subcommand is grouped by its parent")
	assert.Empty(t, featureGroup(topLevel), "an unwrapped top-level command has no group")
}
