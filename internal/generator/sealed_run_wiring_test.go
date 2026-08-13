package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Issue #21 made every cmd.go wire `RunE: … Run<Name>(…)`. Issue #17 made a
// sealed path never created. Together they broke a real project: keryx had
// sealed pkg/cmd/voice/lexicon/main.go and deleted it — the ignore file says
// why, "its cmd.go wires no RunE" — so after regeneration cmd.go referenced a
// RunLexicon that could not be created, and the package stopped compiling:
//
//	pkg/cmd/voice/lexicon/cmd.go:28:11: undefined: RunLexicon (typecheck)
//
// A seal forbids creating main.go AND forbids injecting a stub into it, so the
// only way to keep the project buildable is not to emit the reference.

func sealedRunFixture(t *testing.T, ignoreFile string, mainExists bool) (*Generator, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"
	cmdDir := filepath.Join(root, "pkg", "cmd", "lexicon")

	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), DefaultDirMode))

	if ignoreFile != "" {
		require.NoError(t, afero.WriteFile(fs,
			filepath.Join(root, ".gtb", "ignore"), []byte(ignoreFile), DefaultFileMode))
	}

	if mainExists {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "main.go"),
			[]byte("package lexicon\n"), DefaultFileMode))
	}

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	return New(p, &Config{Path: root, Name: "lexicon"}), cmdDir
}

func TestRunTargetUnreachable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ignoreFile string
		mainExists bool
		want       bool
	}{
		{
			name:       "sealed and absent: nothing can ever define Run",
			ignoreFile: "pkg/cmd/lexicon/main.go sealed\n",
			want:       true,
		},
		{
			// The seal forbids writing, but the file is there, so the function
			// it defines is reachable.
			name:       "sealed but present",
			ignoreFile: "pkg/cmd/lexicon/main.go sealed\n",
			mainExists: true,
			want:       false,
		},
		{
			// 0188 D2: a plain rule still allows creation precisely so the
			// reference stays resolvable.
			name:       "plain ignore rule and absent: it will still be created",
			ignoreFile: "pkg/cmd/lexicon/main.go\n",
			want:       false,
		},
		{
			name: "no rule at all",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := sealedRunFixture(t, tt.ignoreFile, tt.mainExists)
			assert.Equal(t, tt.want, g.runTargetUnreachable(cmdDir))
		})
	}
}

func TestGenerateCommandFile_SealedAbsentMainProducesNoDanglingReference(t *testing.T) {
	t.Parallel()

	g, cmdDir := sealedRunFixture(t, "pkg/cmd/lexicon/main.go sealed\n", false)

	data := &templates.CommandData{Package: "lexicon", Name: "lexicon", PascalName: "Lexicon"}
	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, data))

	got, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "cmd.go"))
	require.NoError(t, err)

	assert.NotContains(t, string(got), "RunLexicon(",
		"cmd.go must not call a function the seal prevents from existing")

	exists, err := afero.Exists(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.False(t, exists, "and the seal is still honoured")
}

func TestGenerateCommandFile_UnsealedStillWiresRun(t *testing.T) {
	t.Parallel()

	g, cmdDir := sealedRunFixture(t, "", false)

	data := &templates.CommandData{Package: "lexicon", Name: "lexicon", PascalName: "Lexicon"}
	require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, data))

	got, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "cmd.go"))
	require.NoError(t, err)

	assert.Contains(t, string(got), "RunLexicon(",
		"the ordinary case is unchanged: #21's wiring stands")
}
