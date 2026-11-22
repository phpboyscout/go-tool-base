package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Two siblings with one package name make the parent import both under the
// same alias, and the project stops compiling (#189).
func TestPrepareAndVerify_RefusesASiblingPackageCollision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		parent  string
		wantErr bool
	}{
		{"a keyword beside its suffixed form", "typecmd", "", true},
		{"hyphen beside underscore", "my_cmd", "", true},
		{"under a parent", "my-cmd", "group", true},
		{"the same command again is not a collision", "my-cmd", "", false},
		{"a distinct name", "other", "", false},
		{"a collision under another parent is not a sibling", "my_cmd", "group", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll("/work/pkg/cmd/group", 0o755))

			raw, err := yaml.Marshal(Manifest{Commands: []ManifestCommand{
				{Name: "type"},
				{Name: "my-cmd"},
				{Name: "group", Commands: []ManifestCommand{{Name: "my_cmd"}}},
			}})
			require.NoError(t, err)
			require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", raw, 0o644))

			g := New(&props.Props{FS: fs, Logger: logger.NewNoop(), Config: emptyTestStore(t)},
				&Config{Path: "/work", Name: tc.command, Parent: tc.parent})

			_, err = g.prepareAndVerify()
			if !tc.wantErr {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrPackageNameCollision)
			assert.Contains(t, err.Error(), commandPackageName(tc.command))
		})
	}
}
