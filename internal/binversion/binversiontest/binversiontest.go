// Package binversiontest builds Go binaries whose build info records a chosen
// version, for tests of the check that reads it back.
package binversiontest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
)

// Shape is how a binary is built, which decides what it records.
type Shape int

const (
	// Tagged records the tag at HEAD.
	Tagged Shape = iota
	// Dirty records the tag with a +dirty suffix.
	Dirty
	// Untagged records a pseudo-version.
	Untagged
	// NoVCS records (devel).
	NoVCS
)

const (
	dirPerm  = 0o700
	filePerm = 0o600
)

type key struct {
	version string
	shape   Shape
}

var (
	mu    sync.Mutex
	built = map[key][]byte{}
)

// Binary is a binary built in shape from a repository tagged version, built
// once per test process.
func Binary(t *testing.T, version string, shape Shape) []byte {
	t.Helper()

	bin, err := Build(t.Context(), version, shape)
	require.NoError(t, err)

	return bin
}

// Build is [Binary] for a caller with no testing.T.
func Build(ctx context.Context, version string, shape Shape) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()

	k := key{version, shape}
	if bin, ok := built[k]; ok {
		return bin, nil
	}

	bin, err := build(ctx, version, shape)
	if err != nil {
		return nil, err
	}

	built[k] = bin

	return bin, nil
}

func build(ctx context.Context, version string, shape Shape) ([]byte, error) {
	dir, err := os.MkdirTemp("", "binversion")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, dirPerm); err != nil {
		return nil, errors.WithStack(err)
	}

	out := filepath.Join(dir, "bin")

	if err := writeSource(repo, "package main\n\nfunc main() {}\n"); err != nil {
		return nil, err
	}

	if err := prepare(ctx, repo, version, shape); err != nil {
		return nil, err
	}

	vcs := "-buildvcs=true"
	if shape == NoVCS {
		vcs = "-buildvcs=false"
	}

	if err := run(ctx, repo, "go", "build", "-trimpath", "-ldflags=-s -w", vcs, "-o", out, "."); err != nil {
		return nil, err
	}

	bin, err := os.ReadFile(out)

	return bin, errors.WithStack(err)
}

func prepare(ctx context.Context, repo, version string, shape Shape) error {
	steps := [][]string{
		{"git", "init", "-q"},
		{"git", "add", "."},
		{"git", "commit", "-q", "-m", "init"},
	}
	if shape != Untagged {
		steps = append(steps, []string{"git", "tag", version})
	}

	for _, s := range steps {
		if err := run(ctx, repo, s[0], s[1:]...); err != nil {
			return err
		}
	}

	if shape == Dirty {
		return writeSource(repo, "package main\n\nfunc main() { println() }\n")
	}

	return nil
}

func writeSource(repo, main string) error {
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/stamped\n\ngo 1.26\n"), filePerm); err != nil {
		return errors.WithStack(err)
	}

	return errors.WithStack(os.WriteFile(filepath.Join(repo, "main.go"), []byte(main), filePerm))
}

// run drops the caller's Go and git settings, which would change the stamp.
func run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir

	cmd.Env = append(slices.DeleteFunc(os.Environ(), repositoryEnv),
		"GOFLAGS=", "GOWORK=off", "CGO_ENABLED=0", "GOOS=", "GOARCH=",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.test",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.test",
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(err, "%s %v: %s", name, args, out)
	}

	return nil
}

// repositoryEnv matches the variables a git hook sets, which would point git
// at the repository the tests run in.
func repositoryEnv(kv string) bool {
	return strings.HasPrefix(kv, "GIT_")
}
