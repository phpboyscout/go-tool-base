package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// nfpm is the version testdata/generate.sh builds the fixtures with.
const nfpm = "github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.43.1"

// The two Debians are the oldest supported and the newest: apt has changed
// what it makes of a path with .. in it, and of a signing key.
var (
	debians = []string{"debian:12-slim", "debian:testing-slim"}
	fedora  = "fedora:latest"
)

// TestInstallIntegration installs gtb with apt and dnf from a repository this
// command indexed, over HTTP, signed by a throwaway key (spec 0097). It needs
// docker and the network.
func TestInstallIntegration(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "packages")

	s := newStore(t)
	s.release(t, "v1.0.0", realPackages(t))
	s.publish(t, "v1.0.0", january)
	s.put(t, "gtb.asc", s.key.public)

	server := httptest.NewServer(http.FileServerFS(os.DirFS(s.dir)))
	t.Cleanup(server.Close)

	for _, image := range debians {
		t.Run(image, func(t *testing.T) { installWithApt(t, newBox(t, image, server.URL)) })
	}

	t.Run(fedora, func(t *testing.T) { installWithDNF(t, newBox(t, fedora, server.URL)) })

	// The last cases, since they change the store.
	s.put(t, "apt/Packages", append(s.get(t, "apt/Packages"), "\n"...))
	require.NoError(t, os.Remove(filepath.Join(s.dir, "apt", "Packages.gz")))
	s.put(t, "rpm/repodata/repomd.xml", append(s.get(t, "rpm/repodata/repomd.xml"), "\n"...))

	for _, image := range debians {
		t.Run(image+" refuses a changed Packages", func(t *testing.T) {
			b := newBox(t, image, server.URL)
			b.run(t, aptSource(b.store))

			code, out := b.exec(t, "apt-get update 2>&1 && apt-get install -y gtb 2>&1")
			assert.NotZero(t, code, out)
			assert.Contains(t, out, "File has unexpected size")
			b.run(t, "! command -v gtb")
		})
	}

	t.Run(fedora+" refuses a changed repomd.xml", func(t *testing.T) {
		b := newBox(t, fedora, server.URL)
		b.run(t, dnfRepo(b.store))

		code, out := b.exec(t, "dnf -y --disablerepo='*' --enablerepo=gtb install gtb 2>&1")
		assert.NotZero(t, code, out)
		assert.Contains(t, out, "repomd.xml GPG signature verification error")
		b.run(t, "! command -v gtb")
	})
}

func installWithApt(t *testing.T, b box) {
	t.Helper()

	b.run(t, aptSource(b.store))

	out := b.run(t, "apt-get update 2>&1")
	assert.NotContains(t, out, "W:")
	assert.NotContains(t, out, "E:")

	b.run(t, "apt-get install -y gtb")
	b.checkInstalled(t, "sudo apt-get install --only-upgrade gtb")
}

func installWithDNF(t *testing.T, b box) {
	t.Helper()

	b.run(t, dnfRepo(b.store))
	b.run(t, "dnf -y --disablerepo='*' --enablerepo=gtb install gtb")
	// What dnf searches by, from filelists and primary.
	assert.Contains(t, b.run(t, "dnf -q --disablerepo='*' --enablerepo=gtb provides /usr/bin/gtb"), "gtb-1.0.0-1")
	b.checkInstalled(t, "sudo dnf upgrade gtb")
}

func (b box) checkInstalled(t *testing.T, upgrade string) {
	t.Helper()

	assert.Contains(t, b.run(t, "/usr/bin/gtb version 2>&1"), "1.0.0")

	code, out := b.exec(t, "/usr/bin/gtb update 2>&1")
	assert.NotZero(t, code, out)
	assert.Contains(t, out, upgrade)
}

// The key stays an .asc file, which signed-by takes by its name.
func aptSource(store string) string {
	return fmt.Sprintf(`apt-get update -qq && apt-get install -y -qq ca-certificates curl >/dev/null
curl -fsS %[1]s/gtb.asc -o /etc/apt/keyrings/gtb.asc
rm -f /etc/apt/sources.list.d/* /etc/apt/sources.list
echo 'deb [signed-by=/etc/apt/keyrings/gtb.asc] %[1]s/apt/ ./' >/etc/apt/sources.list.d/gtb.list`, store)
}

func dnfRepo(store string) string {
	return fmt.Sprintf(`cat >/etc/yum.repos.d/gtb.repo <<EOF
[gtb]
name=gtb
baseurl=%[1]s/rpm/
enabled=1
gpgcheck=0
repo_gpgcheck=1
gpgkey=%[1]s/gtb.asc
EOF`, store)
}

// box is a container that reaches the store.
type box struct {
	container testcontainers.Container
	store     string
}

func newBox(t *testing.T, image, server string) box {
	t.Helper()

	u, err := url.Parse(server)
	require.NoError(t, err)

	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)

	c, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: image, Cmd: []string{"sleep", "infinity"}, HostAccessPorts: []int{port},
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, c)
	require.NoError(t, err)

	return box{container: c, store: fmt.Sprintf("http://%s:%d", testcontainers.HostInternal, port)}
}

func (b box) exec(t *testing.T, script string) (int, string) {
	t.Helper()

	code, r, err := b.container.Exec(t.Context(), []string{"sh", "-ec", script}, tcexec.Multiplexed())
	require.NoError(t, err)

	out, err := io.ReadAll(r)
	require.NoError(t, err)

	return code, string(out)
}

func (b box) run(t *testing.T, script string) string {
	t.Helper()

	code, out := b.exec(t, script)
	require.Zero(t, code, "%s\n%s", script, out)

	return out
}

// realPackages are gtb's own packages for this machine, built as
// .goreleaser.yaml builds them.
func realPackages(t *testing.T) map[string][]byte {
	t.Helper()

	dir := t.TempDir()
	command(t, filepath.Join("..", ".."), nil, "go", "build", "-o", filepath.Join(dir, "gtb"),
		"-ldflags", "-s -w -X gitlab.com/phpboyscout/go-tool-base/cli/pkg/version.version=1.0.0", "./cli/cmd/gtb")

	config := fmt.Sprintf(`name: gtb
arch: %s
version: 1.0.0
maintainer: "Test <test@example.test>"
description: gtb
license: MIT
contents:
  - {src: ./gtb, dst: /usr/bin/gtb, file_info: {mode: 0755}}
`, runtime.GOARCH)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "nfpm.yaml"), []byte(config), 0o600))

	for _, format := range []string{"deb", "rpm"} {
		command(t, dir, []string{"GOWORK=off"}, "go", "run", nfpm, "package", "-p", format)
	}

	files := map[string][]byte{}

	for _, pattern := range []string{"*.deb", "*.rpm"} {
		found, err := filepath.Glob(filepath.Join(dir, pattern))
		require.NoError(t, err)
		require.Len(t, found, 1)

		data, err := os.ReadFile(found[0])
		require.NoError(t, err)

		files[filepath.Base(found[0])] = data
	}

	return files
}

func command(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir, cmd.Env = dir, append(os.Environ(), env...)

	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s\n%s", strings.Join(cmd.Args, " "), out)
}
