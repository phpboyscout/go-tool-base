package workspace

import (
	"os"
	"path/filepath"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
)

// DefaultMaxDepth is the maximum number of parent directories to search
// before giving up. Prevents runaway scanning on deeply nested paths.
const DefaultMaxDepth = 100

// ErrNotFound is returned when no marker file is found before reaching
// the filesystem root or the max depth.
var ErrNotFound = errors.New("workspace not found: no marker file detected")

// DefaultMarkers is the standard set of marker files used to detect
// project boundaries. Checked in order — the first match wins.
var DefaultMarkers = []string{
	".gtb/manifest.yaml", // GTB-generated project
	"go.mod",             // Go module root
	".git",               // Git repository root
}

// Workspace represents a detected project boundary.
type Workspace struct {
	// Root is the absolute path to the project root directory.
	Root string
	// Marker is the marker file or directory that was found
	// (e.g. ".gtb/manifest.yaml", "go.mod", ".git").
	Marker string
}

// Option configures the Detect function.
type Option func(*detectConfig)

type detectConfig struct {
	maxDepth int
}

// WithMaxDepth sets the maximum number of parent directories to search.
// Default: DefaultMaxDepth (100).
func WithMaxDepth(depth int) Option {
	return func(c *detectConfig) { c.maxDepth = depth }
}

// Detect walks up from startDir looking for any of the given marker files.
// Returns the first match. Returns ErrNotFound if no marker is found
// before reaching the filesystem root or the max depth.
//
// Markers are checked in order at each directory level — the first match
// wins. This means ".gtb/manifest.yaml" takes precedence over "go.mod"
// when using DefaultMarkers.
func Detect(fs afero.Fs, startDir string, markers []string, opts ...Option) (*Workspace, error) {
	cfg := &detectConfig{maxDepth: DefaultMaxDepth}
	for _, o := range opts {
		o(cfg)
	}

	dir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, errors.Wrap(err, "resolving start directory")
	}

	for depth := range cfg.maxDepth {
		_ = depth

		for _, marker := range markers {
			path := filepath.Join(dir, marker)

			if _, statErr := fs.Stat(path); statErr == nil {
				return &Workspace{Root: dir, Marker: marker}, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}

		dir = parent
	}

	return nil, ErrNotFound
}

// DetectFromCWD is a convenience that calls Detect starting from the
// current working directory.
func DetectFromCWD(fs afero.Fs, markers []string, opts ...Option) (*Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, errors.Wrap(err, "getting current directory")
	}

	return Detect(fs, cwd, markers, opts...)
}
