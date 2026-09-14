package generator

import "gitlab.com/phpboyscout/go/errors"

var (
	// ErrNotGoToolBaseProject is the placeholder-free sentinel for "no
	// .gtb/manifest.yaml here". Call sites attach the offending path via
	// errors.Wrapf so errors.Is keeps matching.
	ErrNotGoToolBaseProject      = errors.NewSentinel("gtb.generator.not_go_tool_base_project", "the current project is not a gtb project (.gtb/manifest.yaml not found)")
	ErrParentPathNotFound        = errors.NewSentinel("gtb.generator.parent_path_not_found", "parent path not found in manifest")
	ErrModuleNotFound            = errors.NewSentinel("gtb.generator.module_not_found", "could not find module name in go.mod")
	ErrFuncNotFound              = errors.NewSentinel("gtb.generator.func_not_found", "target function not found")
	ErrParentCommandFileNotFound = errors.NewSentinel("gtb.generator.parent_command_file_not_found", "parent command file not found")
)
