package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"

	"github.com/phpboyscout/gtb/pkg/chat"
)

var (
	ErrBuildFailed        = errors.New("build failed")
	ErrTestFailed         = errors.New("tests failed")
	ErrGoGetFailed        = errors.New("go get failed")
	ErrLinterFailed       = errors.New("lint issues found")
	ErrGoModTidyFailed    = errors.New("go mod tidy failed")
	ErrPathInvalid        = errors.New("path is outside of allowed directory")
	ErrInvalidPackageName = errors.New("invalid package name")
)

func isPathAllowed(basePath, requestedPath string) (bool, error) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return false, fmt.Errorf("failed to resolve absolute base path: %w", err)
	}

	absReq, err := filepath.Abs(requestedPath)
	if err != nil {
		return false, fmt.Errorf("failed to resolve absolute requested path: %w", err)
	}

	// Ensure the requested path is within the base path
	if !strings.HasPrefix(absReq, absBase) {
		return false, nil
	}

	return true, nil
}

func ensurePathAllowed(basePath, targetPath string) error {
	allowed, err := isPathAllowed(basePath, targetPath)
	if err != nil {
		return err
	}

	if !allowed {
		return ErrPathInvalid
	}

	return nil
}

func createSingleDirTool(name, description, successMsg string, command []string, failureErr error, basePath string) chat.Tool {
	return chat.Tool{
		Name:        name,
		Description: description,
		Parameters: jsonschema.Reflect(struct {
			Dir string `json:"dir" jsonschema:"description=The directory to run the command in"`
		}{}),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var params struct {
				Dir string `json:"dir"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}

			if err := ensurePathAllowed(basePath, params.Dir); err != nil {
				return nil, err
			}

			cmd := exec.CommandContext(ctx, command[0], command[1:]...) //nolint:gosec // intentional command execution by agent
			cmd.Dir = params.Dir

			output, err := cmd.CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("%w:\n%s", failureErr, string(output))
			}

			return successMsg, nil
		},
	}
}

// ReadFileTool returns a tool for reading file content.
func ReadFileTool(basePath string) chat.Tool {
	return chat.Tool{
		Name:        "read_file",
		Description: "Reads the content of a file at the given path.",
		Parameters: jsonschema.Reflect(struct {
			Path string `json:"path" jsonschema:"description=The absolute path to the file to read"`
		}{}),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}

			if err := ensurePathAllowed(basePath, params.Path); err != nil {
				return nil, err
			}

			content, err := os.ReadFile(params.Path)
			if err != nil {
				return nil, fmt.Errorf("failed to read file: %w", err)
			}

			return string(content), nil
		},
	}
}

// WriteFileTool returns a tool for writing content to a file.
func WriteFileTool(basePath string) chat.Tool {
	return chat.Tool{
		Name:        "write_file",
		Description: "Writes content to a file at the given path, creating it if it doesn't exist.",
		Parameters: jsonschema.Reflect(struct {
			Path    string `json:"path" jsonschema:"description=The absolute path to the file"`
			Content string `json:"content" jsonschema:"description=The content to write"`
		}{}),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var params struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}

			if err := ensurePathAllowed(basePath, params.Path); err != nil {
				return nil, err
			}

			const filePerm = 0600
			if err := os.WriteFile(params.Path, []byte(params.Content), filePerm); err != nil {
				return nil, fmt.Errorf("failed to write file: %w", err)
			}

			return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
		},
	}
}

// ListDirTool returns a tool for listing directory contents.
func ListDirTool(basePath string) chat.Tool {
	return chat.Tool{
		Name:        "list_dir",
		Description: "Lists the files and subdirectories in a given directory.",
		Parameters: jsonschema.Reflect(struct {
			Path string `json:"path" jsonschema:"description=The absolute path to the directory"`
		}{}),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var params struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}

			if err := ensurePathAllowed(basePath, params.Path); err != nil {
				return nil, err
			}

			entries, err := os.ReadDir(params.Path)
			if err != nil {
				return nil, fmt.Errorf("failed to list directory: %w", err)
			}

			var result []string

			for _, entry := range entries {
				name := entry.Name()
				if entry.IsDir() {
					name += "/"
				}

				result = append(result, name)
			}

			return strings.Join(result, "\n"), nil
		},
	}
}

// GoBuildTool returns a tool for running 'go build ./...'.
func GoBuildTool(basePath string) chat.Tool {
	return createSingleDirTool(
		"go_build",
		"Runs 'go build ./...' in the specified directory to check for compilation errors.",
		"Build successful",
		[]string{"go", "build", "./..."},
		ErrBuildFailed,
		basePath,
	)
}

// GoTestTool returns a tool for running 'go test ./...'.
func GoTestTool(basePath string) chat.Tool {
	return createSingleDirTool(
		"go_test",
		"Runs 'go test ./...' in the specified directory to check for test failures.",
		"Tests passed",
		[]string{"go", "test", "./..."},
		ErrTestFailed,
		basePath,
	)
}

// GoGetTool returns a tool for running 'go get'.
func GoGetTool(basePath string) chat.Tool {
	return chat.Tool{
		Name:        "go_get",
		Description: "Runs 'go get <package>' to add or update dependencies.",
		Parameters: jsonschema.Reflect(struct {
			Package string `json:"package" jsonschema:"description=The package to get (e.g. github.com/spf13/cobra)"`
			Dir     string `json:"dir" jsonschema:"description=The directory to run the command in"`
		}{}),
		Handler: func(ctx context.Context, args json.RawMessage) (any, error) {
			var params struct {
				Package string `json:"package"`
				Dir     string `json:"dir"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("invalid arguments: %w", err)
			}

			if err := ensurePathAllowed(basePath, params.Dir); err != nil {
				return nil, err
			}

			// Validate params.Package to prevent command injection
			validPackage := regexp.MustCompile(`^[a-zA-Z0-9_\-./@]+$`)
			if !validPackage.MatchString(params.Package) {
				return nil, errors.Wrap(ErrInvalidPackageName, params.Package)
			}

			cmd := exec.CommandContext(ctx, "go", "get", params.Package) //nolint:gosec // intentional command execution by agent
			cmd.Dir = params.Dir

			output, err := cmd.CombinedOutput()
			if err != nil {
				return nil, fmt.Errorf("%w: %w\nOutput: %s", ErrGoGetFailed, err, string(output))
			}

			return fmt.Sprintf("Successfully got %s", params.Package), nil
		},
	}
}

// LinterTool returns a tool for running 'golangci-lint run --fix'.
func LinterTool(basePath string) chat.Tool {
	return createSingleDirTool(
		"golangci_lint",
		"Runs 'golangci-lint run --fix' in the specified directory to find and fix lint issues.",
		"Linting passed (no issues or all issues fixed)",
		[]string{"golangci-lint", "run", "--fix"},
		ErrLinterFailed,
		basePath,
	)
}

// GoModTidyTool returns a tool for running 'go mod tidy'.
func GoModTidyTool(basePath string) chat.Tool {
	return createSingleDirTool(
		"go_mod_tidy",
		"Runs 'go mod tidy' in the specified directory to clean up go.mod and go.sum.",
		"Go mod tidy successful",
		[]string{"go", "mod", "tidy"},
		ErrGoModTidyFailed,
		basePath,
	)
}
