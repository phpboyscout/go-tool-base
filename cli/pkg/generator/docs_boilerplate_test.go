package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIReferenceNote(t *testing.T) {
	t.Parallel()

	priv := newPromptGenerator(t, "", false).apiReferenceNote("pkg/config", "example.com/tool")
	assert.Contains(t, priv, "go doc ./pkg/config")
	assert.NotContains(t, priv, "https://pkg.go.dev", "private module must not get a registry link")

	pub := newPromptGenerator(t, "", true).apiReferenceNote("pkg/config", "example.com/tool")
	assert.Contains(t, pub, "https://pkg.go.dev/example.com/tool/pkg/config")
}

func TestWritePackageDocStub_ExplanationSkeleton(t *testing.T) {
	t.Parallel()

	g := newPromptGenerator(t, "", false)
	out := "/work/docs/explanation/components/config.md"
	require.NoError(t, g.writePackageDocStub("config", "pkg/config", "example.com/tool", out))

	data, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)
	s := string(data)

	// A real explanation skeleton, not the old "AI required" warning.
	for _, want := range []string{"# config", "## Overview", "## Key Types", "## Usage", "## API Reference", "go doc ./pkg/config"} {
		assert.Containsf(t, s, want, "package stub should contain %q", want)
	}
	assert.NotContains(t, s, "AI Integration Required")
}

func TestWriteBasicCommandDocs_HelpPointer(t *testing.T) {
	t.Parallel()

	g := newPromptGenerator(t, "", false)
	out := "/work/docs/reference/cli/deploy.md"
	require.NoError(t, g.writeBasicCommandDocs("deploy", "mytool deploy", out))

	data, err := afero.ReadFile(g.props.FS, out)
	require.NoError(t, err)
	s := string(data)

	assert.Contains(t, s, "## Usage")
	assert.Contains(t, s, "mytool deploy --help", "command boilerplate should point to --help")
}
