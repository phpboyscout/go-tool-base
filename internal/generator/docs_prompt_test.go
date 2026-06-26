package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func newPromptGenerator(t *testing.T, manifestYAML string, publicAPI bool) *Generator {
	t.Helper()

	const root = "/work"
	fs := afero.NewMemMapFs()
	if manifestYAML != "" {
		require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
		require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "manifest.yaml"), []byte(manifestYAML), 0o644))
	}

	return &Generator{
		props:  &props.Props{FS: fs, Logger: logger.NewNoop()},
		config: &Config{Path: root, PublicAPI: publicAPI},
	}
}

func TestAPIReferencePolicy(t *testing.T) {
	t.Parallel()

	t.Run("private by default: go doc stub, no pkg.go.dev", func(t *testing.T) {
		t.Parallel()
		got := newPromptGenerator(t, "", false).apiReferencePolicy("pkg/config", "example.com/tool")
		assert.Contains(t, got, "go doc ./pkg/config")
		assert.NotContains(t, got, "https://pkg.go.dev", "private policy must not emit a registry URL")
	})

	t.Run("public via --public-api flag: pkg.go.dev link", func(t *testing.T) {
		t.Parallel()
		got := newPromptGenerator(t, "", true).apiReferencePolicy("pkg/config", "example.com/tool")
		assert.Contains(t, got, "https://pkg.go.dev/example.com/tool/pkg/config")
	})

	t.Run("public via manifest module_published: pkg.go.dev link", func(t *testing.T) {
		t.Parallel()
		manifest := "properties:\n  name: tool\n  module_published: true\n"
		got := newPromptGenerator(t, manifest, false).apiReferencePolicy("pkg/config", "example.com/tool")
		assert.Contains(t, got, "https://pkg.go.dev/example.com/tool/pkg/config")
		assert.NotContains(t, got, "go doc")
	})
}

func TestPackagePrompt_IsExplanationOriented(t *testing.T) {
	t.Parallel()

	p := packageDocumentationSystemPrompt
	// Explanation-quadrant sections present.
	for _, want := range []string{"explanation quadrant", "## Overview", "## Key Types", "## API Reference"} {
		assert.Truef(t, strings.Contains(p, want), "package prompt should mention %q", want)
	}
	// The old auto-generated API-dump sections are gone.
	for _, gone := range []string{"## Index", "## Functions", "List of exported symbols"} {
		assert.Falsef(t, strings.Contains(p, gone), "package prompt should no longer dump the API via %q", gone)
	}
}
