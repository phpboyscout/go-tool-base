package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// A parent generated before its first child is a leaf, so its page went to
// <path>.md; once it has a child the index links <path>/index.md, and back
// again when its last child is removed (#138).
func TestMoveCommandDocForShape(t *testing.T) {
	t.Parallel()

	const leafPage, parentPage = "docs/reference/cli/deploy.md", "docs/reference/cli/deploy/index.md"

	cases := map[string]struct {
		parts       []string
		hasChildren bool
		manifest    string
		ignore      string
		existing    map[string]string
		wantFiles   map[string]string
		wantGone    []string
		wantDirs    []string
	}{
		"a first child moves the leaf page to the subsection index": {
			parts:       []string{"deploy"},
			hasChildren: true,
			existing:    map[string]string{leafPage: "deploy docs"},
			wantFiles:   map[string]string{parentPage: "deploy docs"},
			wantGone:    []string{leafPage},
		},
		"a nested parent moves beside its own parent": {
			parts:       []string{"deploy", "start"},
			hasChildren: true,
			existing:    map[string]string{"docs/reference/cli/deploy/start.md": "start docs"},
			wantFiles:   map[string]string{"docs/reference/cli/deploy/start/index.md": "start docs"},
			wantGone:    []string{"docs/reference/cli/deploy/start.md"},
		},
		"losing the last child moves it back and drops the empty subsection": {
			parts:       []string{"deploy"},
			hasChildren: false,
			existing:    map[string]string{parentPage: "deploy docs"},
			wantFiles:   map[string]string{leafPage: "deploy docs"},
			wantGone:    []string{parentPage, "docs/reference/cli/deploy"},
		},
		"a subsection holding anything else is kept when the page moves out": {
			parts:     []string{"deploy"},
			existing:  map[string]string{parentPage: "deploy docs", "docs/reference/cli/deploy/diagram.png": "png"},
			wantFiles: map[string]string{leafPage: "deploy docs", "docs/reference/cli/deploy/diagram.png": "png"},
			wantGone:  []string{parentPage},
		},
		"a page already at the new path is never overwritten": {
			parts:       []string{"deploy"},
			hasChildren: true,
			existing:    map[string]string{leafPage: "old", parentPage: "hand-written"},
			wantFiles:   map[string]string{parentPage: "hand-written", leafPage: "old"},
		},
		"the flat layout has one path for both, so nothing moves": {
			parts:       []string{"deploy"},
			hasChildren: true,
			manifest:    "properties:\n  name: mytool\ncommands:\n  - name: deploy\n",
			existing:    map[string]string{leafPage: "deploy docs"},
			wantFiles:   map[string]string{leafPage: "deploy docs"},
			wantGone:    []string{parentPage},
		},
		"an ignored page is left where it is": {
			parts:       []string{"deploy"},
			hasChildren: true,
			ignore:      "docs/**\n",
			existing:    map[string]string{leafPage: "deploy docs"},
			wantFiles:   map[string]string{leafPage: "deploy docs"},
			wantGone:    []string{parentPage},
		},
		"an ignored leaf page stays, though the index path is free": {
			parts:       []string{"deploy"},
			hasChildren: true,
			ignore:      "docs/reference/cli/deploy.md\n",
			existing:    map[string]string{leafPage: "deploy docs"},
			wantFiles:   map[string]string{leafPage: "deploy docs"},
			wantGone:    []string{parentPage},
		},
		"an ignored index path is never written, though the leaf page is free": {
			parts:       []string{"deploy"},
			hasChildren: true,
			ignore:      "docs/reference/cli/deploy/index.md\n",
			existing:    map[string]string{leafPage: "deploy docs"},
			wantFiles:   map[string]string{leafPage: "deploy docs"},
			wantGone:    []string{parentPage},
		},
		"an ignored subsection is kept when the page moves out of it": {
			parts:     []string{"deploy"},
			ignore:    "docs/reference/cli/deploy\n",
			existing:  map[string]string{parentPage: "deploy docs"},
			wantFiles: map[string]string{leafPage: "deploy docs"},
			wantDirs:  []string{"docs/reference/cli/deploy"},
		},
		"no page, nothing to move": {
			parts:       []string{"deploy"},
			hasChildren: true,
			wantGone:    []string{leafPage, parentPage},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			manifest := tc.manifest
			if manifest == "" {
				manifest = diataxisPathManifest
			}

			g, root := newDocsPathGenerator(t, manifest)
			fs := g.props.FS

			if tc.ignore != "" {
				require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "ignore"), []byte(tc.ignore), 0o644))
			}

			for rel, body := range tc.existing {
				require.NoError(t, afero.WriteFile(fs, filepath.Join(root, rel), []byte(body), 0o644))
			}

			m := g.readManifestQuiet()
			require.NotNil(t, m)
			g.moveCommandDocForShape(m, tc.parts, tc.hasChildren)

			for rel, body := range tc.wantFiles {
				got, err := afero.ReadFile(fs, filepath.Join(root, rel))
				require.NoError(t, err, rel)
				assert.Equal(t, body, string(got), rel)
			}

			for _, rel := range tc.wantDirs {
				isDir, err := afero.IsDir(fs, filepath.Join(root, rel))
				require.NoError(t, err, rel)
				assert.True(t, isDir, rel)
			}

			for _, rel := range tc.wantGone {
				exists, err := afero.Exists(fs, filepath.Join(root, rel))
				require.NoError(t, err)
				assert.False(t, exists, rel)
			}
		})
	}
}

// A regenerate runs the docs step for every command, so a page a previous
// generator left at the leaf path is repaired, not shadowed by a stub.
func TestHandleDocumentationGeneration_RepairsAPageLeftAtTheLeafPath(t *testing.T) {
	t.Parallel()

	g, root := newDocsPathGenerator(t, diataxisPathManifest)
	fs := g.props.FS
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "docs/reference/cli/deploy.md"), []byte("hand-edited"), 0o644))

	g.handleDocumentationGeneration(t.Context(), templates.CommandData{Name: "deploy"}, filepath.Join(root, "pkg/cmd/deploy"), false)

	got, err := afero.ReadFile(fs, filepath.Join(root, "docs/reference/cli/deploy/index.md"))
	require.NoError(t, err)
	assert.Equal(t, "hand-edited", string(got))

	exists, err := afero.Exists(fs, filepath.Join(root, "docs/reference/cli/deploy.md"))
	require.NoError(t, err)
	assert.False(t, exists)
}

// Under regenerate the staged FS records a Remove as a recursive delete, so
// the move must not remove a subsection that still holds anything.
func TestMoveCommandDocForShape_StagedKeepsASubsectionThatIsNotEmpty(t *testing.T) {
	t.Parallel()

	const root = "/work"

	mem := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(mem, filepath.Join(root, ".gtb", "manifest.yaml"), []byte(diataxisPathManifest), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(root, "docs/reference/cli/deploy/index.md"), []byte("deploy docs"), 0o644))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(root, "docs/reference/cli/deploy/diagram.png"), []byte("png"), 0o644))

	staged := newStagedFS(mem)
	g := &Generator{
		props:  &props.Props{FS: staged, Logger: logger.NewNoop(), Tool: props.Tool{Name: "mytool"}},
		config: &Config{Path: root},
	}

	m := g.readManifestQuiet()
	require.NotNil(t, m)
	g.moveCommandDocForShape(m, []string{"deploy"}, false)
	require.NoError(t, staged.materialise())

	assert.Equal(t, "deploy docs", readOrAbsent(t, mem, filepath.Join(root, "docs/reference/cli/deploy.md")))
	assert.Equal(t, "png", readOrAbsent(t, mem, filepath.Join(root, "docs/reference/cli/deploy/diagram.png")))
}
