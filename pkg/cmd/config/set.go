package config

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	cfg "gitlab.com/phpboyscout/go/config"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// setArgCount is the exact number of positional arguments required by "config set".
const setArgCount = 2

// NewCmdSet returns the "config set <key> <value>" subcommand.
func NewCmdSet(props *p.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Write a single configuration value by its dot-notation key.

The value is type-coerced: booleans (true/false) and integers are stored as
their native types; everything else is stored as a string.`,
		Args: cobra.ExactArgs(setArgCount),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, rawVal := args[0], args[1]

			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			// The default config dir may not exist yet (tools without
			// InitCmd); a missing file is a target Apply creates, but its
			// directory must exist.
			ensureDefaultConfigDir(props)

			if err := applyAndHarden(cmd.Context(), props, cfg.Set(key, coerceValue(rawVal))); err != nil {
				return errors.Wrap(err, "persisting config value")
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "set %s = %s\n", key, rawVal)

			return nil
		},
	}

	return cmd
}

// ensureDefaultConfigDir creates the tool's default config directory so a
// write routed to the default path has somewhere to land. Best-effort: a
// custom --config path's directory already exists, because its file loaded.
func ensureDefaultConfigDir(props *p.Props) {
	_, _ = setup.EnsureDefaultConfigDir(writableFS(props), props.Tool.Name)
}

// applyAndHarden commits changes through the store, then re-asserts owner-only
// (0600) permissions on every file the write touched.
//
// go/config preserves an existing file's mode on write — it treats the mode as
// the owner's choice and refuses to quietly tighten it. GTB's policy is
// stricter: a config file routinely holds credentials and must never be group-
// or world-readable (the R4 hardening invariant the setup wizards enforce), so
// the credential-writing commands re-tighten after every write. A file created
// fresh is already 0600, making the chmod idempotent, and it is best-effort
// because some filesystems (afero memfs) don't track modes.
//
// The plan is taken only to learn the target files; a planning error must not
// mask an otherwise successful write, so hardening is skipped rather than made
// fatal in that case.
func applyAndHarden(ctx context.Context, props *p.Props, changes ...cfg.Change) error {
	plan, planErr := props.Config.Plan(changes...)

	if _, err := props.Config.Apply(ctx, changes...); err != nil {
		return err
	}

	if planErr != nil {
		// The write already succeeded; the plan is only used to locate the
		// files to re-harden, so a planning failure just skips hardening rather
		// than reporting a write error that did not happen.
		return nil //nolint:nilerr // deliberate: planErr must not mask a successful Apply
	}

	fs := writableFS(props)
	seen := make(map[string]bool, len(plan.Operations))

	for _, op := range plan.Operations {
		path := op.Target.Name
		if path == "" || seen[path] {
			continue
		}

		seen[path] = true

		_ = fs.Chmod(path, writtenConfigFilePerm)
	}

	return nil
}

// resolveWritableConfigPath returns the file a config write would land in, or
// the user's default config path when the store has no writable layer. The
// answer comes from planning a probe Set against the store — the same routing
// Apply uses — so a declared-but-not-yet-created config file is reported
// correctly rather than falling back to whichever file happened to load.
func resolveWritableConfigPath(props *p.Props, fs afero.Fs) string {
	if props.Config != nil {
		if plan, err := props.Config.Plan(cfg.Set("gtb.write-probe", true)); err == nil && len(plan.Operations) > 0 {
			return plan.Operations[0].Target.Name
		}
	}

	dir := setup.GetDefaultConfigDir(fs, props.Tool.Name)
	if dir == "" {
		return ""
	}

	return filepath.Join(dir, setup.DefaultConfigFilename)
}

// writableFS returns props.FS, defaulting to the real OS filesystem.
func writableFS(props *p.Props) afero.Fs {
	if props.FS != nil {
		return props.FS
	}

	return afero.NewOsFs()
}

// coerceValue attempts to parse s as bool then int64; falls back to string.
func coerceValue(s string) any {
	if b, err := strconv.ParseBool(s); err == nil {
		return b
	}

	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	return s
}
