package generator

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunSkeletonPostProcessing_DeclinesWithoutATool (spec 0200 D5): a
// missing go or golangci-lint is a declined step with a stated reason, not
// an exec error, and the missing binary is never run.
func TestRunSkeletonPostProcessing_DeclinesWithoutATool(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/proj"})
	require.NoError(t, g.props.FS.MkdirAll("/proj/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(g.props.FS, "/proj/.gtb/manifest.yaml", []byte("properties:\n  name: p\n"), 0o644))

	var ran []string

	g.runCommand = func(_ context.Context, _, name string, _ ...string) ([]byte, error) {
		ran = append(ran, name)

		return nil, nil
	}
	g.lookPath = func(name string) (string, error) {
		if name == "go" {
			return "", errors.New("not found")
		}

		return "/usr/bin/" + name, nil
	}

	failed := g.runSkeletonPostProcessing(context.Background(), "/proj")

	assert.Equal(t, []string{"not verified: no Go toolchain on PATH"}, failed)
	assert.Equal(t, []string{"golangci-lint"}, ran, "tidy is declined, lint still runs")
}

func TestRunSkeletonPostProcessing_DeclinesLintWithoutTheBinary(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/proj"})
	require.NoError(t, g.props.FS.MkdirAll("/proj/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(g.props.FS, "/proj/.gtb/manifest.yaml", []byte("properties:\n  name: p\n"), 0o644))

	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	g.lookPath = func(name string) (string, error) {
		if name == "golangci-lint" {
			return "", errors.New("not found")
		}

		return "/usr/bin/" + name, nil
	}

	failed := g.runSkeletonPostProcessing(context.Background(), "/proj")
	assert.Equal(t, []string{"not verified: golangci-lint not on PATH"}, failed)
}
