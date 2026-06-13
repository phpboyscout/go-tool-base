package generator

import "github.com/cockroachdb/errors"

var (
	// ErrNotGoToolBaseProject is the placeholder-free sentinel for "no
	// .gtb/manifest.yaml here". Call sites attach the offending path via
	// errors.Wrapf so errors.Is keeps matching.
	ErrNotGoToolBaseProject      = errors.New("the current project is not a gtb project (.gtb/manifest.yaml not found)")
	ErrParentPathNotFound        = errors.New("parent path not found in manifest")
	ErrModuleNotFound            = errors.New("could not find module name in go.mod")
	ErrFuncNotFound              = errors.New("target function not found")
	ErrParentCommandFileNotFound = errors.New("parent command file not found")
)
