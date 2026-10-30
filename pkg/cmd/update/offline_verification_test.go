package update_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func releaseArchive(t *testing.T, name string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("new binary")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}))
	_, err := tw.Write(body)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// #146: the command's own offline updater honours the user's verification
// settings. It used to be built without them, so both refusals in
// UpdateFromFile were unreachable from the CLI.
func TestUpdateFromFile_HonoursRequiredVerification(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		setting string
		want    string
	}{
		{"require_checksum", "update.require_checksum is enabled but no checksum sidecar was found"},
		{"require_signature", "update.require_signature is enabled but the offline update path cannot verify signatures"},
	} {
		t.Run(tc.setting, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/tmp/release.tar.gz", releaseArchive(t, "test-tool"), 0o644))

			store, err := config.NewStore(t.Context(), config.WithReaders(
				config.NamedSource{Name: "user", Content: []byte("update:\n  " + tc.setting + ": true\n")},
			))
			require.NoError(t, err)

			props := &p.Props{
				FS:      fs,
				Tool:    p.Tool{Name: "test-tool"},
				Logger:  logger.NewNoop(),
				Config:  store,
				Version: version.NewInfo("v1.0.0", "head", "now"),
			}

			cmd := update.NewCmdUpdate(props)
			cmd.SetArgs([]string{"--from-file", "/tmp/release.tar.gz"})

			err = cmd.Execute()
			require.Error(t, err, "an unverifiable archive must not be installed")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
