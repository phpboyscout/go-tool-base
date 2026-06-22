package generate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestNewCmdMan_FlagDefaults(t *testing.T) {
	t.Parallel()

	cmd := NewCmdMan(&props.Props{Logger: logger.NewNoop()})

	dir, _ := cmd.Flags().GetString("dir")
	section, _ := cmd.Flags().GetString("section")
	assert.Equal(t, "./man", dir)
	assert.Equal(t, "1", section)
	assert.NotNil(t, cmd.Flags().Lookup("source"))
	assert.NotNil(t, cmd.Flags().Lookup("manual"))
	assert.NotNil(t, cmd.Flags().Lookup("date"))
}

func TestManOptions_Run_GeneratesPages(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Logger:  logger.NewNoop(),
		Version: version.Info{Version: "9.9.9"},
		Tool:    props.Tool{Name: "demo"},
	}

	// Attach the man command under a fake root so cmd.Root() has a tree.
	root := &cobra.Command{Use: "demo"}
	man := NewCmdMan(p)
	root.AddCommand(man)

	dir := t.TempDir()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"man", "--dir", dir})

	require.NoError(t, root.Execute())
	assert.Contains(t, buf.String(), "wrote man pages to")

	page := filepath.Join(dir, "man1", "demo.1")
	require.FileExists(t, page)
	data, err := os.ReadFile(page)
	require.NoError(t, err)
	// Source defaults to "<tool> <version>".
	assert.Contains(t, string(data), "demo 9.9.9")
}

func TestListManFiles(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "demo"}
	build := &cobra.Command{Use: "build", Run: func(*cobra.Command, []string) {}}
	root.AddCommand(build)

	var buf bytes.Buffer
	require.NoError(t, listManFiles(&buf, root, "./out", "1"))

	out := buf.String()
	assert.Contains(t, out, filepath.Join("out", "man1", "demo.1"))
	assert.Contains(t, out, filepath.Join("out", "man1", "demo-build.1"))
}

func TestParseManDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		wantNil bool
		wantErr bool
	}{
		{"empty is nil", "", true, false},
		{"date-only", "2026-06-21", false, false},
		{"rfc3339", "2026-06-21T10:00:00Z", false, false},
		{"garbage", "not-a-date", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseManDate(tt.in)
			if tt.wantErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantNil, got == nil)
		})
	}
}
