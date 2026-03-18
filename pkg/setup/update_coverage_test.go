package setup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	nurl "net/url"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/charmbracelet/log"
	"github.com/google/go-github/v80/github"
	mockRelease "github.com/phpboyscout/gtb/mocks/pkg/vcs/release"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/version"
	"github.com/phpboyscout/gtb/pkg/vcs/release"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// Helper to create a tar.gz buffer
func createTarGz(t *testing.T, filename, content string) []byte {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	header := &tar.Header{
		Name: filename,
		Mode: 0755,
		Size: int64(len(content)),
	}

	err := tw.WriteHeader(header)
	require.NoError(t, err)
	_, err = tw.Write([]byte(content))
	require.NoError(t, err)

	err = tw.Close()
	require.NoError(t, err)
	err = gw.Close()
	require.NoError(t, err)

	return buf.Bytes()
}

func TestUpdate_Success(t *testing.T) {
	// Setup Mock FS
	memFS := afero.NewMemMapFs()

	// Setup Mocks for Executable
	origOsExecutable := osExecutable
	origExecLookPath := execLookPath

	defer func() {
		osExecutable = origOsExecutable
		execLookPath = origExecLookPath
	}()

	toolName := "test-tool"
	currentBin := "/usr/local/bin/" + toolName

	err := memFS.MkdirAll(filepath.Dir(currentBin), 0755)
	require.NoError(t, err)

	// Create dummy old binary in memFS?
	// Actually osExecutable returns OS path.
	// execLookPath returns OS path.
	// resolveTargetPath calls osExecutable.
	// We mock osExecutable to return a path.
	// But extractAndInstallBinary uses s.Fs to write to that path.
	// Does MEMFS support absolute paths? Yes.

	osExecutable = func() (string, error) {
		return currentBin, nil
	}
	execLookPath = func(file string) (string, error) {
		return currentBin, nil
	}

	// Mock GitHub API server
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	client := github.NewClient(nil)
	baseURL, _ := nurl.Parse(server.URL + "/")
	client.BaseURL = baseURL
	client.UploadURL = baseURL

	// Mock endpoints
	// Latest Release

	c := cases.Title(language.Und)
	currentOS := c.String(runtime.GOOS)
	currentArch := runtime.GOARCH
	if currentArch == "amd64" {
		currentArch = "x86_64"
	}
	expectedName := fmt.Sprintf("%s_%s_%s.tar.gz", toolName, currentOS, currentArch)

	mux.HandleFunc("/repos/org/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v1.1.0", "assets": [{"id": 123, "name": "%s", "browser_download_url": "http://download"}]}`, expectedName)
	})

	// Download Asset
	mux.HandleFunc("/repos/org/repo/releases/assets/123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		content := createTarGz(t, toolName, "new-binary")
		w.Write(content)
	})

	// Mock VCS Client
	mockClient := mockRelease.NewMockProvider(t)
	// We mocked the HTTP layer inside github client before, but now we mock ReleaseProvider directly.
	// DownloadReleaseAsset is expected to return a ReadCloser for the asset, like from httptest
	resp, _ := http.Get(server.URL + "/repos/org/repo/releases/assets/123")
	mockReleaseStub := mockRelease.NewMockRelease(t)
	mockReleaseStub.EXPECT().GetName().Return(expectedName)
	mockReleaseStub.EXPECT().GetTagName().Return("v1.1.0")
	mockAsset := mockRelease.NewMockReleaseAsset(t)
	mockAsset.EXPECT().GetID().Return(int64(123))
	mockAsset.EXPECT().GetName().Return(expectedName)
	mockAsset.EXPECT().GetBrowserDownloadURL().Return(server.URL + "/repos/org/repo/releases/assets/123")
	mockReleaseStub.EXPECT().GetAssets().Return([]release.ReleaseAsset{mockAsset})

	mockClient.EXPECT().GetLatestRelease(mock.Anything, "org", "repo").Return(mockReleaseStub, nil)
	mockClient.EXPECT().DownloadReleaseAsset(mock.Anything, "org", "repo", mockAsset).Return(resp.Body, "", nil)

	// Setup Updater
	props := &props.Props{
		Tool: props.Tool{
			Name: toolName,
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "org",
				Repo:  "repo",
			},
		},
		Config: nil,
		Logger: log.New(io.Discard),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	updater := &SelfUpdater{
		Tool:           props.Tool,
		force:          false,
		version:        "", // Check latest
		logger:         props.Logger,
		releaseClient:  mockClient,
		CurrentVersion: "v1.0.0",
		NextRelease:    nil,
		Fs:             memFS,
	}

	// Mock config directory for timestamps
	// SetTimeSinceLast uses GetDefaultConfigDir which uses os.UserHomeDir (real OS).
	// We need to ensure the path returned by GetDefaultConfigDir exists in MEMFS for Create to work?
	// afero.Fs.Create creates file. Note: parent dir must exist?
	// afero.MemMapFs creates directories recursively on file creation?
	// Actually MapFs implicitly handles directories, but MemMapFs structs?
	// "If you are using MemMapFs, simple Create is fine."
	// Wait, setup/update.go:
	// SetTimeSinceLast checks os.Stat. Wait, it uses fs.Stat now (Step 551).
	// GetDefaultConfigDir(name)
	// fs.Stat(lastSinceFile)
	// fs.Create(lastSinceFile)

	// We should probably create the config dir in memFS to be safe, or check if MemMapFs enforces mkdir.
	// MemMapFs is hierarchical, so mkdir is usually needed.
	configDir := GetDefaultConfigDir(memFS, toolName)
	err = memFS.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	// Enable Logger output
	var logBuf bytes.Buffer
	props.Logger = log.NewWithOptions(&logBuf, log.Options{Level: log.DebugLevel})
	updater.logger = props.Logger

	// Run Update
	updatedPath, err := updater.Update(context.Background())
	if err != nil {
		t.Logf("Update failed. Log output:\n%s", logBuf.String())
	}
	assert.NoError(t, err)
	assert.Equal(t, currentBin, updatedPath)

	// Verify file updated in MemFS
	content, err := afero.ReadFile(memFS, currentBin)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(content))

	// Verify timestamps set
	// CheckedKey
	keyPath := filepath.Join(configDir, "last_checked")
	exists, err := afero.Exists(memFS, keyPath)
	assert.NoError(t, err)
	assert.True(t, exists)

	// Verify GetTimeSinceLast reads from memFS
	duration := GetTimeSinceLast(memFS, toolName, CheckedKey)
	assert.Less(t, duration, time.Minute)
}

func TestSkipUpdateCheck(t *testing.T) {
	memFS := afero.NewMemMapFs()
	toolName := "test-tool"

	cmd := &cobra.Command{Use: "run"}

	// Case 1: Fresh, no last check -> Should return TRUE (skip? wait. Logic is: return timeSinceChecked < 24h)
	// If no file, GetTimeSinceLast returns defaultCheckInterval (24h).
	// 24h < 24h is false. So it returns false (do not skip).
	// Wait, SkipUpdateCheck returns bool.
	// return timeSinceChecked < 24h.
	// If timeSinceChecked is 24h, 24 < 24 is false. Return false (don't skip, do check).

	shouldSkip := SkipUpdateCheck(memFS, toolName, cmd)
	assert.False(t, shouldSkip, "Should not skip if never checked")

	// Case 2: Recently checked
	err := SetTimeSinceLast(memFS, toolName, CheckedKey)
	require.NoError(t, err)

	shouldSkip = SkipUpdateCheck(memFS, toolName, cmd)
	assert.True(t, shouldSkip, "Should skip if recently checked")

	// Case 3: Skippable command
	cmdSkip := &cobra.Command{Use: "version"}
	shouldSkip = SkipUpdateCheck(memFS, toolName, cmdSkip)
	assert.True(t, shouldSkip, "Should skip for skippable command")
}

func TestGetReleaseNotes_Real(t *testing.T) {
	// Setup Mocks
	mockClient := mockRelease.NewMockProvider(t)

	mockRelease1 := mockRelease.NewMockRelease(t)
	mockRelease1.EXPECT().GetTagName().Return("v1.2.0")
	mockRelease1.On("GetBody").Return("New feature").Maybe()
	mockRelease1.EXPECT().GetDraft().Return(false)

	mockRelease2 := mockRelease.NewMockRelease(t)
	mockRelease2.EXPECT().GetTagName().Return("v1.1.0")
	mockRelease2.On("GetBody").Return("Fix stuff").Maybe()
	mockRelease2.EXPECT().GetDraft().Return(false)

	mockRelease3 := mockRelease.NewMockRelease(t)
	mockRelease3.EXPECT().GetTagName().Return("v1.0.0")
	mockRelease3.On("GetBody").Return("Initial").Maybe()
	mockRelease3.EXPECT().GetDraft().Return(false)

	mockClient.EXPECT().ListReleases(mock.Anything, "org", "repo", 100).Return([]release.Release{
		mockRelease1, mockRelease2, mockRelease3,
	}, nil).Once()

	updater := &SelfUpdater{
		Tool: props.Tool{
			ReleaseSource: props.ReleaseSource{Type: "github", Owner: "org", Repo: "repo"},
		},
		releaseClient: mockClient,
	}

	// Test
	notes, err := updater.GetReleaseNotes(context.Background(), "v1.0.0", "v1.2.0")
	require.NoError(t, err)
	assert.Contains(t, notes, "New feature")
	assert.Contains(t, notes, "Fix stuff")
	assert.Contains(t, notes, "# v1.1.0")
	assert.Contains(t, notes, "# v1.2.0")

	// Test No notes
	mockClient.EXPECT().ListReleases(mock.Anything, "org", "repo", 100).Return([]release.Release{}, nil).Once()
	notes, err = updater.GetReleaseNotes(context.Background(), "v1.0.0", "v1.2.0")
	require.NoError(t, err)
	assert.Contains(t, notes, "No release notes found between")
}
