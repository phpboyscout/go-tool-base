package pkgmanager_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/exectest"
	"gitlab.com/phpboyscout/go-tool-base/internal/pkgmanager"
)

// owner answers an ownership query as the named tool owning pkg would, and
// fails it as every other tool does.
func owner(tool, answer string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		if strings.HasSuffix(name, tool) {
			return exec.CommandContext(ctx, "echo", answer)
		}

		return exec.CommandContext(ctx, "false")
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		command func(context.Context, string, ...string) *exec.Cmd
		want    pkgmanager.Install
		managed bool
		upgrade string
	}{
		{
			name: "Apple silicon cask", path: "/opt/homebrew/Caskroom/gtb/0.47.0/gtb",
			want:    pkgmanager.Install{Manager: pkgmanager.HomebrewCask, Package: "gtb"},
			managed: true, upgrade: "brew upgrade --cask gtb",
		},
		{
			name: "Intel cask", path: "/usr/local/Caskroom/mytool/1.2.3/mytool",
			want:    pkgmanager.Install{Manager: pkgmanager.HomebrewCask, Package: "mytool"},
			managed: true, upgrade: "brew upgrade --cask mytool",
		},
		{
			name: "Linux formula", path: "/home/linuxbrew/.linuxbrew/Cellar/mytool/1.2.3/bin/mytool",
			want:    pkgmanager.Install{Manager: pkgmanager.HomebrewFormula, Package: "mytool"},
			managed: true, upgrade: "brew upgrade mytool",
		},
		{
			name: "dpkg owns it", path: "/usr/bin/mytool", command: owner("dpkg-query", "mytool: /usr/bin/mytool"),
			want:    pkgmanager.Install{Manager: pkgmanager.Dpkg, Package: "mytool"},
			managed: true, upgrade: "sudo apt-get install --only-upgrade mytool",
		},
		{
			name: "dpkg shared path", path: "/usr/bin/mytool", command: owner("dpkg-query", "mytool, mytool-extra: /usr/bin/mytool"),
			want:    pkgmanager.Install{Manager: pkgmanager.Dpkg, Package: "mytool"},
			managed: true, upgrade: "sudo apt-get install --only-upgrade mytool",
		},
		{
			name: "rpm owns it", path: "/usr/bin/mytool", command: owner("rpm", "mytool"),
			want:    pkgmanager.Install{Manager: pkgmanager.RPM, Package: "mytool"},
			managed: true, upgrade: "sudo dnf upgrade mytool",
		},
		{name: "system path nobody owns", path: "/usr/bin/mytool", command: exectest.FailCommand()},
		{name: "install script", path: "/home/me/.local/bin/mytool"},
		{name: "usr local", path: "/usr/local/bin/mytool"},
		{name: "Cellar with no version", path: "/home/me/Cellar/mytool"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			command := tc.command
			if command == nil {
				command = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
					t.Error("no package database should be queried for this path")

					return exec.CommandContext(ctx, "false")
				}
			}

			d := pkgmanager.New(pkgmanager.WithLookPath(func(f string) (string, error) { return "/usr/bin/" + f, nil }),
				pkgmanager.WithCommand(command))

			got, managed := d.Detect(t.Context(), tc.path)
			require.Equal(t, tc.managed, managed)

			if !managed {
				return
			}

			assert.Equal(t, tc.want.Manager, got.Manager)
			assert.Equal(t, tc.want.Package, got.Package)
			assert.Equal(t, tc.upgrade, got.UpgradeCommand())
		})
	}
}

// A system without dpkg or rpm asks neither.
func TestDetect_NoPackageDatabase(t *testing.T) {
	t.Parallel()

	d := pkgmanager.New(pkgmanager.WithLookPath(exectest.MissingLookPath()),
		pkgmanager.WithCommand(owner("dpkg-query", "mytool: /usr/bin/mytool")))

	_, managed := d.Detect(t.Context(), "/usr/bin/mytool")
	assert.False(t, managed)
}

// Homebrew links a cask's binary into its bin directory; the link is what
// os.Executable may report, so the detector judges the file it points at.
func TestDetect_FollowsSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "Caskroom", "mytool", "1.2.3", "mytool")
	link := filepath.Join(root, "bin", "mytool")

	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.WriteFile(target, nil, 0o600))
	require.NoError(t, os.Symlink(target, link))

	got, managed := pkgmanager.New(pkgmanager.WithLookPath(exectest.MissingLookPath())).Detect(t.Context(), link)
	require.True(t, managed)
	assert.Equal(t, pkgmanager.HomebrewCask, got.Manager)
	assert.Equal(t, "mytool", got.Package)
}
