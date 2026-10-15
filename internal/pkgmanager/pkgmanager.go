// Package pkgmanager tells whether a binary was installed by a package
// manager, so a self-updater can defer to that manager rather than replace a
// file it tracks (gtb#129).
package pkgmanager

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manager names a package manager that can own an installed binary.
type Manager string

const (
	// HomebrewFormula is a binary under a Homebrew Cellar.
	HomebrewFormula Manager = "Homebrew formula"
	// HomebrewCask is a binary under a Homebrew Caskroom.
	HomebrewCask Manager = "Homebrew cask"
	// Dpkg is a file dpkg records as installed by a .deb package.
	Dpkg Manager = "dpkg"
	// RPM is a file the RPM database records as installed by a package.
	RPM Manager = "rpm"
)

// queryTimeout bounds a dpkg or rpm ownership query; a slow package database
// must not hold an update indefinitely.
const queryTimeout = 5 * time.Second

// systemPrefixes are where dpkg and rpm install files. Anything else, such as
// $HOME/.local/bin or /usr/local/bin, is never theirs, so no query is run.
var systemPrefixes = []string{"/usr/", "/bin/", "/sbin/", "/opt/"}

// Install is a binary a package manager owns.
type Install struct {
	Manager Manager
	Package string
	// Path is the binary's path with symlinks resolved.
	Path string
}

// UpgradeCommand is the package manager's own command for upgrading the
// package.
func (i Install) UpgradeCommand() string {
	switch i.Manager {
	case HomebrewCask:
		return "brew upgrade --cask " + i.Package
	case HomebrewFormula:
		return "brew upgrade " + i.Package
	case Dpkg:
		return "sudo apt-get install --only-upgrade " + i.Package
	case RPM:
		return "sudo dnf upgrade " + i.Package
	}

	return ""
}

// Detector finds the package manager, if any, that owns a binary.
type Detector struct {
	evalSymlinks func(string) (string, error)
	lookPath     func(string) (string, error)
	command      func(context.Context, string, ...string) *exec.Cmd
}

// Option configures a [Detector].
type Option func(*Detector)

// WithLookPath replaces exec.LookPath, which finds dpkg-query and rpm.
func WithLookPath(fn func(string) (string, error)) Option {
	return func(d *Detector) { d.lookPath = fn }
}

// WithCommand replaces exec.CommandContext, which runs the ownership query.
func WithCommand(fn func(context.Context, string, ...string) *exec.Cmd) Option {
	return func(d *Detector) { d.command = fn }
}

// New returns a Detector over the real filesystem and package databases.
func New(opts ...Option) *Detector {
	d := &Detector{
		evalSymlinks: filepath.EvalSymlinks,
		lookPath:     exec.LookPath,
		command:      exec.CommandContext,
	}

	for _, o := range opts {
		o(d)
	}

	return d
}

// Detect reports the package manager that installed the binary at exe. A
// Homebrew install is recognised from its path alone; dpkg and rpm are asked,
// and only for a path under a system prefix. A failed query is not ownership.
func (d *Detector) Detect(ctx context.Context, exe string) (Install, bool) {
	resolved, err := d.evalSymlinks(exe)
	if err != nil {
		resolved = exe
	}

	if inst, ok := homebrew(resolved); ok {
		return inst, true
	}

	if !underSystemPrefix(resolved) {
		return Install{}, false
	}

	if pkg, ok := d.query(ctx, "dpkg-query", "-S", resolved); ok {
		return Install{Manager: Dpkg, Package: dpkgPackage(pkg), Path: resolved}, true
	}

	if pkg, ok := d.query(ctx, "rpm", "-qf", "--queryformat", "%{NAME}", resolved); ok {
		return Install{Manager: RPM, Package: pkg, Path: resolved}, true
	}

	return Install{}, false
}

// homebrew matches <prefix>/Cellar/<formula>/<version>/... and
// <prefix>/Caskroom/<token>/<version>/..., the layouts brew installs into on
// macOS and Linux alike.
func homebrew(path string) (Install, bool) {
	segs := strings.Split(filepath.ToSlash(path), "/")

	for i := 0; i+3 < len(segs); i++ {
		var m Manager

		switch segs[i] {
		case "Cellar":
			m = HomebrewFormula
		case "Caskroom":
			m = HomebrewCask
		default:
			continue
		}

		if segs[i+1] != "" && segs[i+2] != "" {
			return Install{Manager: m, Package: segs[i+1], Path: path}, true
		}
	}

	return Install{}, false
}

func underSystemPrefix(path string) bool {
	for _, p := range systemPrefixes {
		if strings.HasPrefix(path, p) && !strings.HasPrefix(path, "/usr/local/") {
			return true
		}
	}

	return false
}

// query runs an ownership query, answering the first line of its output when
// it succeeds. Both tools exit non-zero for a file no package owns.
func (d *Detector) query(ctx context.Context, tool string, args ...string) (string, bool) {
	bin, err := d.lookPath(tool)
	if err != nil {
		return "", false
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	out, err := d.command(ctx, bin, args...).Output()
	if err != nil {
		return "", false
	}

	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if line == "" {
		return "", false
	}

	return line, true
}

// dpkgPackage takes the package from a `dpkg-query -S` line, "pkg: /path" or
// "pkg1, pkg2: /path" when packages share a path.
func dpkgPackage(line string) string {
	pkgs, _, _ := strings.Cut(line, ": ")
	first, _, _ := strings.Cut(pkgs, ", ")

	return first
}
