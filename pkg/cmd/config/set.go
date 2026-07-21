package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	cfg "gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/redact"

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

			change := cfg.Set(key, coerceValue(rawVal))

			// Writing a recognised credential into a project-local config file
			// risks committing the secret to version control. Warn — and, when
			// interactive, ask to confirm — but never block: a project-local
			// secret can be legitimate.
			proceed, err := confirmSensitiveProjectLocalWrite(cmd, props, key, rawVal, change)
			if err != nil {
				return err
			}

			if !proceed {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "aborted")

				return nil
			}

			if err := applyAndHarden(cmd.Context(), props, change); err != nil {
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

// confirmSensitiveProjectLocalWrite guards a credential write that would land
// in a project-local config file — the committable ".<tool>.yaml" at a repo
// root, which the store routes to ahead of the user's private config when one
// is present. It returns whether the write should proceed.
//
// The write is never blocked outright: a project-local secret can be
// deliberate. When it is a recognised credential, the user is warned; if the
// session is interactive they are asked to confirm, and a decline aborts.
// A non-interactive session cannot be prompted, so it proceeds after the
// warning — CI and scripts are not held hostage to a TTY.
func confirmSensitiveProjectLocalWrite(cmd *cobra.Command, props *p.Props, key, value string, change cfg.Change) (bool, error) {
	target := plannedWriteTarget(props, change)
	if !isProjectLocalConfig(target, props.Tool.Name) || !isSensitiveWrite(key, value) {
		return true, nil
	}

	w := cmd.ErrOrStderr()
	_, _ = fmt.Fprintf(w, "\nWarning: %q looks like a credential and would be written to\n  %s\n", key, target)
	_, _ = fmt.Fprintln(w, "a project-local config file that may be committed to version control.")
	_, _ = fmt.Fprintln(w, "Prefer env-var or OS-keychain storage (see the tool's `init`), or write it to")
	_, _ = fmt.Fprintln(w, "your private config with an explicit --config path.")
	_, _ = fmt.Fprintln(w)

	if !isInteractiveInput(cmd) {
		_, _ = fmt.Fprintln(w, "Proceeding (non-interactive); re-run in a terminal to be asked to confirm.")

		return true, nil
	}

	var confirmed bool

	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Write this credential to the project-local config file?").
				Affirmative("Yes, write it").
				Negative("No, cancel").
				Value(&confirmed),
		),
	).Run(); err != nil {
		return false, errors.Wrap(err, "confirmation cancelled")
	}

	return confirmed, nil
}

// plannedWriteTarget returns the file a change would be routed to, or "" when
// the store cannot plan it. Unlike resolveWritableConfigPath's new-key probe,
// this routes the actual change, so a key that already lives in a lower-
// precedence file reports that file.
func plannedWriteTarget(props *p.Props, change cfg.Change) string {
	if props.Config == nil {
		return ""
	}

	plan, err := props.Config.Plan(change)
	if err != nil || len(plan.Operations) == 0 {
		return ""
	}

	return plan.Operations[0].Target.Name
}

// isProjectLocalConfig reports whether path is a project-local ".<tool>.yaml"
// config file — the repo-root layer discovered by walking up from the working
// directory. The global config file is named config.yaml, so the base name
// cleanly distinguishes the two regardless of directory.
func isProjectLocalConfig(path, toolName string) bool {
	if path == "" || toolName == "" {
		return false
	}

	return filepath.Base(path) == "."+toolName+".yaml"
}

// isSensitiveWrite reports whether writing value under key would place a
// credential in the file. Two independent signals: the value matches a known
// credential shape (redact rewrites it — sk-/ghp_/AIza/glpat- prefixes, JWTs,
// long opaque tokens), or the key is one GTB recognises as a literal-credential
// slot (the migrate catalogue). The value signal catches secrets under a
// downstream tool's own keys; the key signal catches a short or unusual secret
// under a known slot. Reference-mode keys (.env, .keychain) hold an env-var
// name or keychain locator, not a secret, so neither signal fires for them.
func isSensitiveWrite(key, value string) bool {
	if redact.String(value) != value {
		return true
	}

	return isKnownCredentialKey(key)
}

// isKnownCredentialKey reports whether key is one GTB recognises as holding a
// literal credential — the catalogue config migrate scans, including the
// Bitbucket dual-credential pair whose halves live outside knownCredentials.
func isKnownCredentialKey(key string) bool {
	for _, c := range knownCredentials {
		if key == c.key {
			return true
		}
	}

	return key == bitbucketPrimary.key || key == bitbucketPartner.key
}

// isInteractiveInput reports whether the command's input is an interactive
// terminal. It reads the command's own input stream — os.Stdin by default, so
// production behaviour is unchanged — which lets tests force the
// non-interactive path with cmd.SetIn.
func isInteractiveInput(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
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
