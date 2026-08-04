package config

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/google/shlex"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// editTempSuffix is appended to the config path to form the sibling temp file
// the editor operates on. The round-trip persists to the real path only after
// the edited temp file validates.
const editTempSuffix = ".edit.tmp"

// editFilePerm is the mode for the temp edit file — 0600, matching the
// credential-bearing config file it stands in for.
const editFilePerm = 0o600

// editorRunner launches an editor (argv, already split) against path and waits
// for it to exit. Injected for tests so no real editor or terminal is needed.
type editorRunner func(ctx context.Context, argv []string, path string) error

// editConfig holds the injectable seams for "config edit".
type editConfig struct {
	runEditor   editorRunner
	interactive func() bool
}

// EditOption configures NewCmdEdit. Used by tests to inject a fake editor and
// interactivity check; production uses the defaults.
type EditOption func(*editConfig)

// WithEditorRunner overrides the editor-launch seam (default: a real
// subprocess via exec.CommandContext). Tests inject a deterministic mutator.
func WithEditorRunner(r editorRunner) EditOption {
	return func(c *editConfig) { c.runEditor = r }
}

// WithInteractiveCheck overrides the TTY gate (default: utils.IsInteractive).
// Tests inject a constant so the round-trip runs without a real terminal.
func WithInteractiveCheck(fn func() bool) EditOption {
	return func(c *editConfig) { c.interactive = fn }
}

// NewCmdEdit returns the "config edit" subcommand. It opens the writable config
// file in the user's editor, re-validates the result on save, and persists only
// if it is valid — aborting (and leaving the original untouched) on a non-zero
// editor exit, a YAML syntax error, or a schema validation failure.
func NewCmdEdit(props *p.Props, opts ...EditOption) *cobra.Command {
	ec := &editConfig{runEditor: defaultEditorRunner, interactive: utils.IsInteractive}
	for _, o := range opts {
		o(ec)
	}

	var editorFlag string

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit the configuration file in $EDITOR",
		Long: `Open the writable config file in your editor ($VISUAL, then $EDITOR,
then a platform default), re-validate it when you save, and persist the change
only if it is valid.

On a non-zero editor exit, invalid YAML, or a schema validation failure the
edit is aborted and the original file is left untouched. Your unsaved edit is
preserved at a temp file whose path is reported, so nothing is lost.

Only the writable file layer is edited — values from environment variables or
flags are not file-backed. For scripted, non-interactive changes use
"config set" / "config unset" instead.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			if !ec.interactive() {
				return errors.WithHint(
					errors.New("config edit requires an interactive terminal"),
					"use `config set` / `config unset` for non-interactive or scripted changes",
				)
			}

			return runEdit(cmd, props, ec, editorFlag)
		},
	}

	cmd.Flags().StringVar(&editorFlag, "editor", "",
		"editor command to use (overrides $VISUAL/$EDITOR)")

	return cmd
}

// runEdit performs the temp-file editor round-trip.
func runEdit(cmd *cobra.Command, props *p.Props, ec *editConfig, editorFlag string) error {
	fs := writableFS(props)

	path := resolveWritableConfigPath(props, fs)
	if path == "" {
		return errors.New("could not resolve a writable config path")
	}

	editor := resolveEditor(editorFlag)

	argv, err := shlex.Split(editor)
	if err != nil || len(argv) == 0 {
		return errors.WithHintf(
			errors.Newf("could not parse editor command %q", editor),
			"set a valid $EDITOR or pass --editor",
		)
	}

	original := seedOrRead(fs, path, props.Tool.Name)

	tmpPath := path + editTempSuffix
	if werr := afero.WriteFile(fs, tmpPath, original, editFilePerm); werr != nil {
		return errors.Wrap(werr, "writing temp edit file")
	}

	if rerr := ec.runEditor(cmd.Context(), argv, tmpPath); rerr != nil {
		_ = fs.Remove(tmpPath)

		return errors.Wrap(rerr, "editor exited with an error; config left unchanged")
	}

	edited, err := afero.ReadFile(fs, tmpPath)
	if err != nil {
		return errors.Wrap(err, "reading edited file")
	}

	return persistEdit(cmd, props, fs, path, tmpPath, original, edited)
}

// persistEdit validates the edited bytes and, if valid and changed, writes them
// to path and reloads. On any failure the temp file is retained and its path
// reported so the user's edit is not lost.
func persistEdit(cmd *cobra.Command, props *p.Props, fs afero.Fs, path, tmpPath string, original, edited []byte) error {
	// A save that changed nothing is a no-op regardless of validity — short
	// circuit before parsing or validating.
	if bytes.Equal(original, edited) {
		_ = fs.Remove(tmpPath)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "no changes made")

		return nil
	}

	var probe map[string]any
	if err := yaml.Unmarshal(edited, &probe); err != nil {
		return errors.WithHintf(
			errors.Wrap(err, "edited config is not valid YAML; original left unchanged"),
			"your edit is preserved at %s", tmpPath,
		)
	}

	if err := validateCandidate(cmd.Context(), props, edited); err != nil {
		return errors.WithHintf(err, "your edit is preserved at %s; original left unchanged", tmpPath)
	}

	if err := writeConfigAtomic(fs, path, edited); err != nil {
		return err
	}

	_ = fs.Remove(tmpPath)

	// Re-resolve so subsequent reads see the written state. A no-op when the
	// edited file is not one of the store's declared layers.
	if err := props.Config.Reload(cmd.Context()); err != nil {
		return errors.Wrap(err, "reload config after edit")
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "saved %s\n", path)

	return nil
}

// seedOrRead returns the current file contents, or — when the file does not yet
// exist — an empty document with a header comment. It never seeds the full
// effective config, so embedded defaults are not materialised into the file.
func seedOrRead(fs afero.Fs, path, toolName string) []byte {
	if data, err := afero.ReadFile(fs, path); err == nil {
		return data
	}

	name := toolName
	if name == "" {
		name = "tool"
	}

	return []byte(fmt.Sprintf(
		"# %s configuration\n# Add configuration keys below, then save.\n# Run `config validate` to check the result.\n",
		name,
	))
}

// resolveEditor returns the editor command: the --editor flag, then $VISUAL,
// then $EDITOR, then a platform default (notepad on Windows, vi elsewhere).
func resolveEditor(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}

	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}

	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}

	if runtime.GOOS == "windows" {
		return "notepad"
	}

	return "vi"
}

// defaultEditorRunner launches the editor as a real subprocess against path,
// inheriting the current process's standard streams so the user interacts with
// it directly.
func defaultEditorRunner(ctx context.Context, argv []string, path string) error {
	args := append(append([]string(nil), argv[1:]...), path)

	//nolint:gosec // G204: operator-selected editor ($VISUAL/$EDITOR/--editor) on an operator-named temp file
	cmd := exec.CommandContext(ctx, argv[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// writtenConfigFilePerm is the POSIX mode the rewritten config is
// left in after a successful write. Matches the 0600 invariant
// enforced by the initial setup wizards (R4 in the hardening spec)
// — a credential-bearing file must not be world-readable.
const writtenConfigFilePerm = 0o600

// writeConfigAtomic writes data to path via a temp-file + rename
// dance so a mid-write interrupt leaves the original file intact.
// Also enforces writtenConfigFilePerm on the final file.
func writeConfigAtomic(fs afero.Fs, path string, data []byte) error {
	if fs == nil {
		fs = afero.NewOsFs()
	}

	// Create the parent dir lazily at first write — setup.GetDefaultConfigDir
	// is pure and no longer creates ~/.toolname as a side effect.
	const configDirPerm = 0o700
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := fs.MkdirAll(dir, configDirPerm); err != nil {
			return errors.Wrap(err, "create config directory")
		}
	}

	tmpPath := path + ".migrate.tmp"

	if err := afero.WriteFile(fs, tmpPath, data, writtenConfigFilePerm); err != nil {
		return errors.Wrap(err, "write temporary migrated config")
	}

	if err := fs.Rename(tmpPath, path); err != nil {
		_ = fs.Remove(tmpPath)

		return errors.Wrap(err, "rename migrated config into place")
	}

	// Best-effort chmod: some filesystems (afero memfs) don't track
	// modes. Tests pass; production OsFs always honours chmod. We
	// intentionally don't error out here.
	_ = fs.Chmod(path, writtenConfigFilePerm)

	return nil
}
