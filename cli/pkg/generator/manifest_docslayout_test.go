package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestResolvedDocsLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		given string
		want  string
	}{
		{name: "empty defaults to flat (back-compat)", given: "", want: DocsLayoutFlat},
		{name: "diataxis is honoured", given: DocsLayoutDiataxis, want: DocsLayoutDiataxis},
		{name: "flat is honoured", given: DocsLayoutFlat, want: DocsLayoutFlat},
		{name: "unrecognised falls back to flat", given: "weird", want: DocsLayoutFlat},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := ManifestProperties{DocsLayout: tc.given}
			assert.Equal(t, tc.want, p.ResolvedDocsLayout())
		})
	}
}

func TestDocsLayoutYAMLRoundTrip(t *testing.T) {
	t.Parallel()

	in := ManifestProperties{
		Name:            "mytool",
		DocsLayout:      DocsLayoutDiataxis,
		ModulePublished: true,
	}

	data, err := yaml.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(data), "docs_layout: diataxis")
	assert.Contains(t, string(data), "module_published: true")

	var out ManifestProperties
	require.NoError(t, yaml.Unmarshal(data, &out))
	assert.Equal(t, DocsLayoutDiataxis, out.DocsLayout)
	assert.True(t, out.ModulePublished)
	assert.Equal(t, DocsLayoutDiataxis, out.ResolvedDocsLayout())
}

func TestDocsLayoutOmitEmpty(t *testing.T) {
	t.Parallel()

	// An unset layout / unpublished module must not serialise, so existing
	// manifests stay byte-stable and absent fields resolve to the flat default.
	data, err := yaml.Marshal(ManifestProperties{Name: "mytool"})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "docs_layout")
	assert.NotContains(t, string(data), "module_published")
}
