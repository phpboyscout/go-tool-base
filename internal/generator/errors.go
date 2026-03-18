package generator

import "errors"

var (
	ErrNotGoToolBaseProject      = errors.New("the current project at '%s' is not a gtb project (.gtb/manifest.yaml not found)")
	ErrParentPathNotFound        = errors.New("parent path not found in manifest")
	ErrModuleNotFound            = errors.New("could not find module name in go.mod")
	ErrFuncNotFound              = errors.New("target function not found")
	ErrParentCommandFileNotFound = errors.New("parent command file not found")
)
