package generator

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/phpboyscout/gtb/mocks/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGenerateDocs(t *testing.T) {
	fs := afero.NewMemMapFs()
	logger := log.New(io.Discard)

	// Create a dummy config
	conf := config.NewFilesContainer(nil, fs)

	p := &props.Props{
		FS:     fs,
		Logger: logger,
		Config: conf,
	}

	// Setup manifest and dummy source
	workDir := "/work"
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte("properties:\n  name: test-tool\n"), 0644))

	cmdDir := filepath.Join(workDir, "pkg/cmd/test-cmd")
	require.NoError(t, fs.MkdirAll(cmdDir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "cmd.go"), []byte("package main"), 0644))

	mockClient := chat.NewMockChatClient(t)
	mockClient.On("Chat", mock.Anything, mock.Anything).Return("--- \ntitle: test-cmd\ndescription: test desc\n---\n# test-cmd", nil)

	g := New(p, &Config{Path: workDir})
	g.chatClient = mockClient

	err := g.GenerateDocs(context.Background(), cmdDir, false)
	require.NoError(t, err)

	// Verify doc file created
	docPath := filepath.Join(workDir, "docs/commands/test-cmd/index.md")
	exists, err := afero.Exists(fs, docPath)
	assert.NoError(t, err)
	assert.True(t, exists)

	data, err := afero.ReadFile(fs, docPath)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "# test-cmd")
}
