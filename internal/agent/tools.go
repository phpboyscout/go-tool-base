package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/invopop/jsonschema"
	"github.com/spf13/afero"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
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

const maxSymlinkDepth = 255

// outputHeadLines and outputTailLines cap the number of subprocess-output
// lines folded into wrapped errors. Addresses L-1 from
// docs/development/reports/security-audit-2026-04-17.md — prevents a
// tool that emits a large stderr from flooding the AI context and
// reduces (but does not yet eliminate) the chance of a secret embedded
// deep in output reaching the AI. When the pkg/redact package lands
// per 2026-04-17-telemetry-redaction.md, truncateOutput should route
// the result through redact.String as a follow-up.
const (
	outputHeadLines = 50
	outputTailLines = 50
)

// truncateOutput returns a bounded head+tail representation of b suitable
// for embedding in error messages. When the line count is within the
// head+tail budget, the input is returned unchanged.
func truncateOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) <= outputHeadLines+outputTailLines {
		return strings.Join(lines, "\n")
	}

	omitted := len(lines) - outputHeadLines - outputTailLines

	var sb strings.Builder

	sb.WriteString(strings.Join(lines[:outputHeadLines], "\n"))
	fmt.Fprintf(&sb, "\n… [omitted %d line(s)] …\n", omitted)
	sb.WriteString(strings.Join(lines[len(lines)-outputTailLines:], "\n"))

	return sb.String()
}

// symlinksResolver holds state for component-by-component symlink resolution.
type symlinksResolver struct {
	lstater     afero.Lstater
	reader      afero.LinkReader
	resolved    string
	linksUsed   int
	maxSymlinks int
}

// resolveComponent processes a single path component, following symlinks if needed.
// It returns the updated components slice and index, or an error.
func (r *symlinksResolver) resolveComponent(components []string, i int) ([]string, int, error) {
	comp := components[i]
	candidate := filepath.Join(r.resolved, comp)

	info, lstatCalled, lstatErr := r.lstater.LstatIfPossible(candidate)
	if lstatErr != nil {
		// Path doesn't exist — append remaining components as-is
		for j := i; j < len(components); j++ {
			if components[j] != "" {
				r.resolved = filepath.Join(r.resolved, components[j])
			}
		}

		return components, len(components), nil
	}

	if !lstatCalled || info.Mode()&fs.ModeSymlink == 0 {
		r.resolved = candidate

		return components, i, nil
	}

	return r.followSymlink(candidate, components, i)
}

func (r *symlinksResolver) followSymlink(candidate string, components []string, i int) ([]string, int, error) {
	r.linksUsed++
	if r.linksUsed > r.maxSymlinks {
		return nil, 0, errors.New("too many levels of symlinks")
	}

	target, readErr := r.reader.ReadlinkIfPossible(candidate)
	if readErr != nil {
		return nil, 0, errors.Wrap(readErr, "failed to read symlink")
	}

	targetComponents := strings.Split(filepath.Clean(target), string(filepath.Separator))

	if filepath.IsAbs(target) {
		r.resolved = string(filepath.Separator)
	}

	remaining := make([]string, 0, len(targetComponents)+len(components)-i-1)
	remaining = append(remaining, targetComponents...)
	remaining = append(remaining, components[i+1:]...)

	return remaining, -1, nil
}

// resolveSymlinks resolves symlinks in the given path using the provided filesystem.
// If the filesystem doesn't support symlink operations (e.g., MemMapFs),
// the absolute path is returned unchanged.
func resolveSymlinks(afs afero.Fs, p string) (string, error) {
	absPath, err := filepath.Abs(p)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve absolute path")
	}

	lstater, hasLstat := afs.(afero.Lstater)
	reader, hasReadlink := afs.(afero.LinkReader)

	if !hasLstat || !hasReadlink {
		return absPath, nil
	}

	r := &symlinksResolver{
		lstater:     lstater,
		reader:      reader,
		resolved:    string(filepath.Separator),
		maxSymlinks: maxSymlinkDepth,
	}

	components := strings.Split(filepath.Clean(absPath), string(filepath.Separator))

	for i := 0; i < len(components); i++ {
		if components[i] == "" {
			continue
		}

		var resolveErr error

		components, i, resolveErr = r.resolveComponent(components, i)
		if resolveErr != nil {
			return "", resolveErr
		}
	}

	return r.resolved, nil
}

// isPathAllowed resolves symlinks in both paths via the provided filesystem
// and checks that the requested path falls within the base path. It returns
// the resolved absolute path on success.
func isPathAllowed(afs afero.Fs, basePath, requestedPath string) (string, error) {
	absBase, err := filepath.Abs(basePath)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve absolute base path")
	}

	absReq, err := filepath.Abs(requestedPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to resolve absolute requested path")
	}

	realBase, err := resolveSymlinks(afs, absBase)
	if err != nil {
		return "", errors.Wrap(err, "failed to evaluate symlinks in base path")
	}

	realReq, err := resolveSymlinks(afs, absReq)
	if err != nil {
		// File may not exist yet (write operations) — resolve parent
		parentDir := filepath.Dir(absReq)

		realParent, parentErr := resolveSymlinks(afs, parentDir)
		if parentErr != nil {
			return "", errors.Wrap(parentErr, "failed to evaluate symlinks in parent directory")
		}

		realReq = filepath.Join(realParent, filepath.Base(absReq))
	}

	if !strings.HasPrefix(realReq, realBase+string(filepath.Separator)) && realReq != realBase {
		return "", ErrPathInvalid
	}

	return realReq, nil
}

func ensurePathAllowed(afs afero.Fs, basePath, targetPath string) (string, error) {
	return isPathAllowed(afs, basePath, targetPath)
}

func createSingleDirTool(name, description, successMsg string, command []string, failureErr error, afs afero.Fs, basePath string) chat.Tool {
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
				return nil, errors.Wrap(err, "invalid arguments")
			}

			resolvedDir, err := ensurePathAllowed(afs, basePath, params.Dir)
			if err != nil {
				return nil, err
			}

			cmd := exec.CommandContext(ctx, command[0], command[1:]...) //nolint:gosec // intentional command execution by agent
			cmd.Dir = resolvedDir

			output, err := cmd.CombinedOutput()
			if err != nil {
				return nil, errors.Wrapf(failureErr, "\n%s", truncateOutput(output))
			}

			return successMsg, nil
		},
	}
}

// ReadFileTool returns a tool for reading file content.
func ReadFileTool(afs afero.Fs, basePath string) chat.Tool {
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
				return nil, errors.Wrap(err, "invalid arguments")
			}

			resolvedPath, err := ensurePathAllowed(afs, basePath, params.Path)
			if err != nil {
				return nil, err
			}

			content, err := afero.ReadFile(afs, resolvedPath)
			if err != nil {
				return nil, errors.Wrap(err, "failed to read file")
			}

			return string(content), nil
		},
	}
}

// WriteFileTool returns a tool for writing content to a file.
func WriteFileTool(afs afero.Fs, basePath string) chat.Tool {
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
				return nil, errors.Wrap(err, "invalid arguments")
			}

			resolvedPath, err := ensurePathAllowed(afs, basePath, params.Path)
			if err != nil {
				return nil, err
			}

			const filePerm = 0600
			if err := afero.WriteFile(afs, resolvedPath, []byte(params.Content), filePerm); err != nil {
				return nil, errors.Wrap(err, "failed to write file")
			}

			return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
		},
	}
}

// ListDirTool returns a tool for listing directory contents.
func ListDirTool(afs afero.Fs, basePath string) chat.Tool {
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
				return nil, errors.Wrap(err, "invalid arguments")
			}

			resolvedPath, err := ensurePathAllowed(afs, basePath, params.Path)
			if err != nil {
				return nil, err
			}

			entries, err := afero.ReadDir(afs, resolvedPath)
			if err != nil {
				return nil, errors.Wrap(err, "failed to list directory")
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
func GoBuildTool(afs afero.Fs, basePath string) chat.Tool {
	return createSingleDirTool(
		"go_build",
		"Runs 'go build ./...' in the specified directory to check for compilation errors.",
		"Build successful",
		[]string{"go", "build", "./..."},
		ErrBuildFailed,
		afs,
		basePath,
	)
}

// GoTestTool returns a tool for running 'go test ./...'.
func GoTestTool(afs afero.Fs, basePath string) chat.Tool {
	return createSingleDirTool(
		"go_test",
		"Runs 'go test ./...' in the specified directory to check for test failures.",
		"Tests passed",
		[]string{"go", "test", "./..."},
		ErrTestFailed,
		afs,
		basePath,
	)
}

// GoGetTool returns a tool for running 'go get'.
func GoGetTool(afs afero.Fs, basePath string) chat.Tool {
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
				return nil, errors.Wrap(err, "invalid arguments")
			}

			resolvedDir, err := ensurePathAllowed(afs, basePath, params.Dir)
			if err != nil {
				return nil, err
			}

			// Validate params.Package to prevent command injection
			validPackage := regexp.MustCompile(`^[a-zA-Z0-9_\-./@]+$`)
			if !validPackage.MatchString(params.Package) {
				return nil, errors.Wrap(ErrInvalidPackageName, params.Package)
			}

			cmd := exec.CommandContext(ctx, "go", "get", params.Package) //nolint:gosec // intentional command execution by agent
			cmd.Dir = resolvedDir

			output, err := cmd.CombinedOutput()
			if err != nil {
				return nil, errors.Wrapf(ErrGoGetFailed, "%s\nOutput: %s", err, truncateOutput(output))
			}

			return fmt.Sprintf("Successfully got %s", params.Package), nil
		},
	}
}

// LinterTool returns a tool for running 'golangci-lint run --fix'.
func LinterTool(afs afero.Fs, basePath string) chat.Tool {
	return createSingleDirTool(
		"golangci_lint",
		"Runs 'golangci-lint run --fix' in the specified directory to find and fix lint issues.",
		"Linting passed (no issues or all issues fixed)",
		[]string{"golangci-lint", "run", "--fix"},
		ErrLinterFailed,
		afs,
		basePath,
	)
}

// GoModTidyTool returns a tool for running 'go mod tidy'.
func GoModTidyTool(afs afero.Fs, basePath string) chat.Tool {
	return createSingleDirTool(
		"go_mod_tidy",
		"Runs 'go mod tidy' in the specified directory to clean up go.mod and go.sum.",
		"Go mod tidy successful",
		[]string{"go", "mod", "tidy"},
		ErrGoModTidyFailed,
		afs,
		basePath,
	)
}
