package update_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion/binversiontest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/update"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// relabelledChannel serves v1.1.0 and v1.3.0, with a v1.0.0 binary in both.
func relabelledChannel(t *testing.T) string {
	t.Helper()

	objects := map[string]any{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch body := objects[r.URL.Path].(type) {
		case nil:
			http.NotFound(w, r)
		case []byte:
			_, _ = w.Write(body)
		default:
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
	t.Cleanup(srv.Close)

	archive := forgetest.TarGzAsset("rollback-tool", "rollback-tool", string(binversiontest.Binary(t, "v1.0.0", binversiontest.Tagged)))
	sum := sha256.Sum256(archive.Body)
	previous := ""

	for _, tag := range []string{"v1.1.0", "v1.3.0"} {
		objects["/"+tag+"/"+archive.Name] = archive.Body
		objects["/"+tag+"/"+static.ChecksumsFile] = forgetest.Manifest(false, archive)
		objects["/"+tag+"/"+static.ManifestFile] = static.Manifest{
			Schema: static.SchemaVersion, Tool: "rollback-tool", Tag: tag, Previous: previous,
			ReleasedAt: "2026-11-23T12:00:00Z",
			Checksums:  static.FileURL(srv.URL, tag, static.ChecksumsFile),
			Downloads: []static.Download{{
				OS: runtime.GOOS, Arch: runtime.GOARCH, Name: archive.Name,
				URL: static.FileURL(srv.URL, tag, archive.Name), Size: int64(len(archive.Body)), SHA256: hex.EncodeToString(sum[:]),
			}},
		}
		objects["/"+static.PointerFile] = static.Pointer{
			Schema: static.SchemaVersion, Tool: "rollback-tool", Tag: tag,
			Manifest: static.ManifestURL(srv.URL, tag), PublishedAt: "2026-11-23T12:00:00Z",
		}
		previous = tag
	}

	return srv.URL
}

// --allow-version-mismatch reaches the updater on every route that installs a
// release, and nothing else gets past the check (spec 0220 D8).
func TestNewCmdUpdate_AllowVersionMismatch(t *testing.T) {
	t.Parallel()

	for name, args := range map[string][]string{
		"latest":               {},
		"latest, forced":       {"--force"},
		"a named version":      {"--version=v1.1.0"},
		"a pinned version":     {"--pin=v1.1.0"},
		"a forced rollback":    {"--rollback", "--force"},
		"a forced rollback to": {"--rollback=v1.1.0", "--force"},
	} {
		for _, allow := range []bool{false, true} {
			t.Run(name+map[bool]string{false: " is refused", true: " is allowed"}[allow], func(t *testing.T) {
				t.Parallel()

				store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "user", Content: []byte("{}\n")}))
				require.NoError(t, err)

				props := rollbackProps(unattended())
				props.Config = store
				props.Tool.ReleaseSource = p.ReleaseSource{Type: p.ReleaseSourceStatic, BaseURL: relabelledChannel(t)}

				cmd := update.NewCmdUpdate(props, noRefresh())
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)

				given := slices.Clone(args)
				if allow {
					given = append(given, "--allow-version-mismatch")
				}

				cmd.SetArgs(given)
				err = cmd.Execute()

				target, exeErr := os.Executable()
				require.NoError(t, exeErr)

				installed, existsErr := afero.Exists(props.FS, target)
				require.NoError(t, existsErr)
				assert.Equal(t, allow, installed)

				if allow {
					require.NoError(t, err)

					return
				}

				require.ErrorIs(t, err, setup.ErrVersionMismatch)
				assert.Empty(t, pinned(t, props), "a refused release is not pinned")
			})
		}
	}
}

// Allowing a mismatch is not consent to a rollback (spec 0220 D8).
func TestNewCmdUpdate_AllowVersionMismatchIsNotForce(t *testing.T) {
	t.Setenv("CI", "")

	r := &rollbackUpdater{before: "v1.1.0"}
	cmd := update.NewCmdUpdate(rollbackProps(unattended()), r.factory(), noRefresh())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--rollback=v1.1.0", "--allow-version-mismatch"})

	require.ErrorIs(t, cmd.Execute(), setup.ErrDowngradeNotConfirmed)
	assert.False(t, r.replaced)
}
