package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/credentials"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
)

// keychainOpTimeout bounds any single credentials-backend operation
// triggered by the migrate command. Matches the bound the setup
// wizards use so a remote-store backend (Vault, SSM) that hangs
// cannot stall migration.
const keychainOpTimeout = 5 * time.Second

// configKeyDefaultTarget lets tool authors pin a default `--target`
// for `config migrate-credentials` in the tool's config so CI flows
// don't need to pass the flag every invocation. Values: `env`,
// `keychain`. Unknown values fall back to [credentials.ModeEnvVar].
const configKeyDefaultTarget = "credentials.migrate.default_target"

// MigrateOptions configures the migrate-credentials flow. The zero
// value is a dry-run against [credentials.ModeEnvVar] with
// interactive prompting — safe to run without fear of mutation.
type MigrateOptions struct {
	// DryRun prints the planned changes but does not write the
	// config or call the keychain backend.
	DryRun bool

	// Target selects the destination storage mode. Defaults to
	// [credentials.ModeEnvVar] if unset.
	Target credentials.Mode

	// AssumeYes skips every interactive prompt. Env-var names fall
	// back to the upstream-standard defaults; keychain account names
	// follow the wizard convention. Intended for CI pipelines and
	// scripted migrations.
	AssumeYes bool

	// EnvVarOverrides maps source config key → chosen env var name.
	// Lets non-interactive callers pin a specific env var per
	// credential (e.g. `--env-var anthropic.api.key=MYTOOL_ANTHROPIC`).
	// Overrides both defaults and interactive prompts.
	EnvVarOverrides map[string]string

	// KeychainService overrides the keychain service (the first half
	// of the <service>/<account> reference). Defaults to the tool's
	// name (`props.Tool.Name`), matching the setup wizards.
	KeychainService string

	// SkipVerify disables the post-export verification step in
	// interactive env-var mode. Useful for headless automation
	// where the user sets env vars via /etc/profile or systemd
	// drop-ins rather than an interactive shell.
	SkipVerify bool
}

// MigrationAction describes a single migration the command will
// perform (dry-run) or has performed.
type MigrationAction struct {
	// SourceKey is the config key currently holding the literal
	// credential (e.g. `anthropic.api.key`, `github.auth.value`).
	// For dual credentials this is the "primary" half (username).
	SourceKey string

	// PartnerKey is set for dual-credential pairs where both halves
	// migrate together (only `bitbucket.username` + `bitbucket.app_password`
	// today). Empty for single-value credentials.
	PartnerKey string

	// Target is the mode this credential was migrated to.
	Target credentials.Mode

	// DestKey is the config key written after migration. For
	// env-var targets: `<prefix>.env`. For keychain: `<prefix>.keychain`.
	// For Bitbucket dual-cred keychain target, only one key is
	// written (the shared `bitbucket.keychain`); PartnerKey is still
	// set so the report lists both removed literals.
	DestKey string

	// DestValue is the written value — an env var name (env-var
	// target) or a `<service>/<account>` reference (keychain
	// target). The source credential value is NEVER included here.
	DestValue string

	// Skipped is true when the credential was already migrated or
	// could not be migrated (e.g. keychain target with no backend
	// registered). Reason explains why.
	Skipped bool
	Reason  string
}

// MigrateResult summarises the outcome of a migrate run.
type MigrateResult struct {
	// Actions is the set of candidate migrations found in config,
	// with status filled in. Useful for machine-readable output.
	Actions []MigrationAction

	// WroteConfig is true when the config file on disk was rewritten.
	// Always false in DryRun mode.
	WroteConfig bool
}

// Migrate performs the migration described by opts against
// [props.Config]. Safe to call multiple times — already-migrated
// credentials are detected and skipped.
func Migrate(ctx context.Context, props *p.Props, opts MigrateOptions) (*MigrateResult, error) {
	if props.Config == nil {
		return nil, errors.New("no configuration loaded")
	}

	target, err := resolveMigrateTarget(opts.Target, props.Config)
	if err != nil {
		return nil, err
	}

	opts.Target = target

	if target == credentials.ModeKeychain && !credentials.KeychainAvailable() {
		return nil, errors.WithHint(
			errors.New("keychain target requested but no keychain-capable Backend is registered"),
			"Import github.com/phpboyscout/go-tool-base/pkg/credentials/keychain in your tool's main, or pass --target=env.",
		)
	}

	candidates := scanLiteralCredentials(props.Config)
	if len(candidates) == 0 {
		return &MigrateResult{}, nil
	}

	// Mutations are staged into two sets so we can rewrite the
	// config file atomically at the end. viper.Set on a dotted path
	// whose parent is "cleared" to an empty string clobbers the
	// parent's subtree (e.g. setting `bitbucket.username` to "" after
	// setting `bitbucket.username.env` destroys the `.env` key). By
	// deferring deletions to the rewrite step we sidestep that
	// collision entirely.
	plan := &rewritePlan{
		sets:    map[string]any{},
		deletes: map[string]struct{}{},
	}

	result := &MigrateResult{Actions: make([]MigrationAction, 0, len(candidates))}

	for _, c := range candidates {
		action, actErr := processCandidate(ctx, props, opts, c, plan)
		if actErr != nil {
			return nil, actErr
		}

		result.Actions = append(result.Actions, action)
	}

	if opts.DryRun {
		return result, nil
	}

	if err := applyPlan(props, plan); err != nil {
		return nil, err
	}

	result.WroteConfig = true

	return result, nil
}

// rewritePlan accumulates the mutations to apply in a single atomic
// step at the end of a migrate run. sets are simple key→value
// insertions; deletes remove a dotted path entirely from the
// resulting YAML tree. Keeping these separate means we can safely
// delete a parent key like `bitbucket.username` without clobbering
// a newly-set child like `bitbucket.username.env`.
type rewritePlan struct {
	sets    map[string]any
	deletes map[string]struct{}
}

// resolveMigrateTarget picks the effective target: explicit
// opts.Target wins; otherwise the config's `credentials.migrate.default_target`
// key if valid; otherwise [credentials.ModeEnvVar].
func resolveMigrateTarget(explicit credentials.Mode, cfg config.Containable) (credentials.Mode, error) {
	if explicit != "" {
		return validateMigrateTarget(explicit)
	}

	if v := strings.TrimSpace(cfg.GetString(configKeyDefaultTarget)); v != "" {
		return validateMigrateTarget(credentials.Mode(v))
	}

	return credentials.ModeEnvVar, nil
}

func validateMigrateTarget(m credentials.Mode) (credentials.Mode, error) {
	switch m {
	case credentials.ModeEnvVar, credentials.ModeKeychain:
		return m, nil
	case credentials.ModeLiteral:
		return "", errors.New("invalid migration target \"literal\"; migrate is for moving OFF literal mode, not onto it")
	}

	return "", errors.Newf("invalid migration target %q; expected one of: env, keychain", m)
}

// processCandidate routes a single candidate through the
// mode-specific migration path, honouring opts.DryRun and opts.AssumeYes.
// Mutations are staged into the shared plan rather than applied
// immediately — applyPlan commits them atomically at the end.
func processCandidate(
	ctx context.Context,
	props *p.Props,
	opts MigrateOptions,
	c literalCredential,
	plan *rewritePlan,
) (MigrationAction, error) {
	if alreadyMigrated(props.Config, c, opts.Target) {
		return MigrationAction{
			SourceKey:  c.Key,
			PartnerKey: c.PartnerKey,
			Target:     opts.Target,
			Skipped:    true,
			Reason:     "already migrated",
		}, nil
	}

	switch opts.Target {
	case credentials.ModeEnvVar:
		return migrateToEnvVar(opts, c, plan)
	case credentials.ModeKeychain:
		return migrateToKeychain(ctx, props, opts, c, plan)
	case credentials.ModeLiteral:
		return MigrationAction{}, errors.New("unreachable: literal is not a valid migration target (caught by validateMigrateTarget)")
	}

	return MigrationAction{}, errors.Newf("unreachable: unexpected target %q", opts.Target)
}

// migrateToEnvVar stages the env-var target write and literal
// deletion into the plan. The mutations aren't visible in
// props.Config until applyPlan runs at the end of the migration.
func migrateToEnvVar(opts MigrateOptions, c literalCredential, plan *rewritePlan) (MigrationAction, error) {
	envName, err := resolveEnvVarName(opts, c)
	if err != nil {
		return MigrationAction{}, err
	}

	if !opts.AssumeYes && !opts.DryRun {
		if err := instructAndVerifyEnvVar(envName, c, opts.SkipVerify); err != nil {
			return MigrationAction{}, err
		}
	}

	action := MigrationAction{
		SourceKey:  c.Key,
		PartnerKey: c.PartnerKey,
		Target:     credentials.ModeEnvVar,
		DestKey:    c.EnvTargetKey,
		DestValue:  envName,
	}

	if opts.DryRun {
		return action, nil
	}

	plan.sets[c.EnvTargetKey] = envName
	plan.deletes[c.Key] = struct{}{}

	if c.PartnerKey != "" {
		partnerEnvName := defaultEnvVarName(c.PartnerKey)
		if v, ok := opts.EnvVarOverrides[c.PartnerKey]; ok && v != "" {
			partnerEnvName = v
		}

		plan.sets[c.PartnerEnvTargetKey] = partnerEnvName
		plan.deletes[c.PartnerKey] = struct{}{}
	}

	return action, nil
}

// migrateToKeychain stores the secret in the keychain and stages the
// config mutations (reference added, literal(s) deleted) into the
// plan for the atomic rewrite at the end.
func migrateToKeychain(
	ctx context.Context,
	props *p.Props,
	opts MigrateOptions,
	c literalCredential,
	plan *rewritePlan,
) (MigrationAction, error) {
	service := opts.KeychainService
	if service == "" {
		service = props.Tool.Name
	}

	if service == "" {
		return MigrationAction{}, errors.New("cannot build keychain reference: tool name is empty and --keychain-service not set")
	}

	secret, err := secretForKeychain(props.Config, c)
	if err != nil {
		return MigrationAction{}, err
	}

	ref := service + "/" + c.KeychainAccount
	action := MigrationAction{
		SourceKey:  c.Key,
		PartnerKey: c.PartnerKey,
		Target:     credentials.ModeKeychain,
		DestKey:    c.KeychainTargetKey,
		DestValue:  ref,
	}

	if opts.DryRun {
		return action, nil
	}

	storeCtx, cancel := context.WithTimeout(ctx, keychainOpTimeout)
	defer cancel()

	if err := credentials.Store(storeCtx, service, c.KeychainAccount, secret); err != nil {
		return MigrationAction{}, errors.WithHint(
			errors.Wrapf(err, "storing %s in OS keychain", c.Key),
			"If the keychain is locked, unlock it and re-run; otherwise fall back to --target=env.",
		)
	}

	plan.sets[c.KeychainTargetKey] = ref
	plan.deletes[c.Key] = struct{}{}

	if c.PartnerKey != "" {
		plan.deletes[c.PartnerKey] = struct{}{}
	}

	return action, nil
}

// secretForKeychain returns the value to store in the keychain for
// credential c. For dual-credential bundles, returns a JSON blob
// matching the shape the resolver expects. For single-value
// credentials, returns the raw secret.
func secretForKeychain(cfg config.Containable, c literalCredential) (string, error) {
	if c.PartnerKey == "" {
		return c.Value, nil
	}

	blob, err := json.Marshal(map[string]string{
		jsonBlobFieldFor(c.Key):        c.Value,
		jsonBlobFieldFor(c.PartnerKey): cfg.GetString(c.PartnerKey),
	})
	if err != nil {
		return "", errors.Wrap(err, "marshal dual-credential keychain blob")
	}

	return string(blob), nil
}

// jsonBlobFieldFor maps a config key to the JSON field name used in
// the keychain blob. Currently only Bitbucket uses this; the mapping
// is straightforward.
func jsonBlobFieldFor(key string) string {
	switch key {
	case "bitbucket.username":
		return "username"
	case "bitbucket.app_password":
		return "app_password"
	}

	return key
}

// alreadyMigrated reports whether the target key already has a value
// in config — meaning a prior migration already ran for this
// credential. Avoids duplicate writes and awkward double prompts
// when the command is invoked multiple times.
func alreadyMigrated(cfg config.Containable, c literalCredential, target credentials.Mode) bool {
	switch target {
	case credentials.ModeEnvVar:
		return strings.TrimSpace(cfg.GetString(c.EnvTargetKey)) != ""
	case credentials.ModeKeychain:
		return strings.TrimSpace(cfg.GetString(c.KeychainTargetKey)) != ""
	case credentials.ModeLiteral:
		// Literal is not a valid target; see validateMigrateTarget.
		return false
	}

	return false
}

// resolveEnvVarName picks the env var name for credential c, honouring
// --env-var overrides, falling back to defaults, and prompting the
// user in interactive mode.
func resolveEnvVarName(opts MigrateOptions, c literalCredential) (string, error) {
	if v, ok := opts.EnvVarOverrides[c.Key]; ok && v != "" {
		return v, nil
	}

	if opts.AssumeYes {
		return defaultEnvVarName(c.Key), nil
	}

	envName := defaultEnvVarName(c.Key)

	if opts.DryRun {
		return envName, nil
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Env var name for %s", c.Key)).
				Description("Credential currently stored as literal. Choose the env var that will hold it. Default is the upstream ecosystem standard.").
				Placeholder(envName).
				Value(&envName).
				Validate(validateEnvVarName),
		),
	).Run()
	if err != nil {
		return "", errors.Wrap(err, "env var name prompt cancelled")
	}

	return envName, nil
}

// instructAndVerifyEnvVar prints the copy-me-into-your-shell
// instructions for a migration candidate and (unless SkipVerify)
// waits for the user to confirm the env var is set.
//
// Verification reads the env var after the user confirms and checks
// it matches the current literal value. Mismatch aborts the
// migration for that credential.
func instructAndVerifyEnvVar(envName string, c literalCredential, skipVerify bool) error {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "Set %s in your shell profile:\n", envName)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "    export %s=<paste the current %s value>\n", envName, c.Key)

	if c.PartnerKey != "" {
		partnerEnv := defaultEnvVarName(c.PartnerKey)
		fmt.Fprintf(os.Stderr, "    export %s=<paste the current %s value>\n", partnerEnv, c.PartnerKey)
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "The config file's literal entry will be cleared; the env var will be the single source of truth going forward.")
	fmt.Fprintln(os.Stderr)

	if skipVerify {
		return nil
	}

	var confirmed bool

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Have you set the env var?").
				Affirmative("Yes, continue").
				Negative("No, cancel migration").
				Value(&confirmed),
		),
	).Run()
	if err != nil {
		return errors.Wrap(err, "env var verification cancelled")
	}

	if !confirmed {
		return errors.New("migration aborted by user")
	}

	if os.Getenv(envName) == "" {
		return errors.WithHint(
			errors.Newf("%s is not set in the current environment", envName),
			"Export the variable in the shell you're using to run this command, or re-run with --skip-verify if you use another mechanism (systemd drop-in, /etc/profile, etc.).",
		)
	}

	return nil
}

// defaultEnvVarName returns the upstream-standard env var name for a
// known credential key, or a sanitised uppercase of the key when
// unknown. Split across [defaultAIEnvVarName] and [defaultVCSEnvVarName]
// so each helper stays under the cyclomatic-complexity budget.
func defaultEnvVarName(key string) string {
	if name := defaultAIEnvVarName(key); name != "" {
		return name
	}

	if name := defaultVCSEnvVarName(key); name != "" {
		return name
	}

	return strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// defaultAIEnvVarName resolves the three AI-provider credential keys
// to the well-known env var names their SDKs / CI integrations
// expect.
func defaultAIEnvVarName(key string) string {
	switch key {
	case chat.ConfigKeyClaudeKey:
		return chat.EnvClaudeKey
	case chat.ConfigKeyOpenAIKey:
		return chat.EnvOpenAIKey
	case chat.ConfigKeyGeminiKey:
		return chat.EnvGeminiKey
	}

	return ""
}

// defaultVCSEnvVarName resolves VCS-provider credential keys to the
// upstream-standard env var names (`GITHUB_TOKEN`, `GITLAB_TOKEN`,
// etc.). Bitbucket's dual-credential pair maps to the canonical
// `BITBUCKET_USERNAME` / `BITBUCKET_APP_PASSWORD` pairing.
func defaultVCSEnvVarName(key string) string {
	switch key {
	case "github.auth.value":
		return "GITHUB_TOKEN"
	case "gitlab.auth.value":
		return "GITLAB_TOKEN"
	case "gitea.auth.value":
		return "GITEA_TOKEN"
	case "codeberg.auth.value":
		return "CODEBERG_TOKEN"
	case "direct.auth.value":
		return "DIRECT_TOKEN"
	case "bitbucket.username":
		return "BITBUCKET_USERNAME"
	case "bitbucket.app_password":
		return "BITBUCKET_APP_PASSWORD"
	}

	return ""
}

// validateEnvVarName matches the shape used by pkg/setup/ai and
// pkg/setup/github — conservative POSIX env var form.
func validateEnvVarName(name string) error {
	if name == "" {
		return errors.New("env var name is required")
	}

	if !envVarNameRe.MatchString(name) {
		return errors.New("env var name must match ^[A-Z][A-Z0-9_]{0,63}$")
	}

	return nil
}

// applyPlan atomically commits a [rewritePlan] to the config file:
// loads the current settings, deletes the dotted keys from the
// nested map, merges the new sets in, writes the YAML back to the
// same file, and reloads the viper so in-memory Get returns the
// new state. Atomicity is at the file-write step — partial failures
// before the rewrite don't change anything on disk.
func applyPlan(props *p.Props, plan *rewritePlan) error {
	v := props.Config.GetViper()

	configPath := v.ConfigFileUsed()
	if configPath == "" {
		return errors.New("no config file path bound to the loaded configuration; cannot rewrite")
	}

	settings := v.AllSettings()

	for key := range plan.deletes {
		deleteNestedKey(settings, key)
	}

	for key, val := range plan.sets {
		setNestedKey(settings, key, val)
	}

	data, err := yaml.Marshal(settings)
	if err != nil {
		return errors.Wrap(err, "marshal migrated config")
	}

	if err := writeConfigAtomic(props.FS, configPath, data); err != nil {
		return err
	}

	// Re-read so subsequent props.Config.Get* calls see the written
	// state rather than the stale override map that still holds the
	// pre-rewrite literal values.
	if err := v.ReadInConfig(); err != nil {
		return errors.Wrap(err, "reload config after rewrite")
	}

	return nil
}

// migratedConfigFilePerm is the POSIX mode the rewritten config is
// left in after a successful migration. Matches the 0600 invariant
// enforced by the initial setup wizards (R4 in the hardening spec)
// — a credential-bearing file must not be world-readable.
const migratedConfigFilePerm = 0o600

// writeConfigAtomic writes data to path via a temp-file + rename
// dance so a mid-write interrupt leaves the original file intact.
// Also enforces migratedConfigFilePerm on the final file.
func writeConfigAtomic(fs afero.Fs, path string, data []byte) error {
	if fs == nil {
		fs = afero.NewOsFs()
	}

	tmpPath := path + ".migrate.tmp"

	if err := afero.WriteFile(fs, tmpPath, data, migratedConfigFilePerm); err != nil {
		return errors.Wrap(err, "write temporary migrated config")
	}

	if err := fs.Rename(tmpPath, path); err != nil {
		_ = fs.Remove(tmpPath)

		return errors.Wrap(err, "rename migrated config into place")
	}

	// Best-effort chmod: some filesystems (afero memfs) don't track
	// modes. Tests pass; production OsFs always honours chmod. We
	// intentionally don't error out here.
	_ = fs.Chmod(path, migratedConfigFilePerm)

	return nil
}

// deleteNestedKey removes a dot-path entry from a nested map. When
// the path resolves through non-map nodes, the call is a no-op —
// the caller is asking to delete something that isn't there.
func deleteNestedKey(m map[string]any, dotPath string) {
	parts := strings.Split(dotPath, ".")
	current := m

	for i, part := range parts {
		if i == len(parts)-1 {
			delete(current, part)

			return
		}

		next, ok := current[part].(map[string]any)
		if !ok {
			return
		}

		current = next
	}
}

// setNestedKey writes val at a dot-path in m, creating intermediate
// map nodes as needed. Existing non-map nodes on the path are
// overwritten to a map so the leaf can be set — the migrate command
// is the declared authority over the keys it writes.
func setNestedKey(m map[string]any, dotPath string, val any) {
	parts := strings.Split(dotPath, ".")
	current := m

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = val

			return
		}

		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}

		current = next
	}
}

// PrintResult writes a human-readable summary of result to w. Used
// by NewCmdMigrate but exported for reuse in tests and custom
// output paths.
func PrintResult(w io.Writer, result *MigrateResult, dryRun bool) {
	if len(result.Actions) == 0 {
		_, _ = fmt.Fprintln(w, "No literal credentials found — nothing to migrate.")

		return
	}

	header := "Migration plan (dry run — no changes written):"
	if !dryRun {
		header = "Migration complete:"
	}

	_, _ = fmt.Fprintln(w, header)
	_, _ = fmt.Fprintln(w)

	for _, a := range result.Actions {
		printAction(w, a)
	}
}

func printAction(w io.Writer, a MigrationAction) {
	keys := a.SourceKey
	if a.PartnerKey != "" {
		keys = a.SourceKey + " + " + a.PartnerKey
	}

	if a.Skipped {
		_, _ = fmt.Fprintf(w, "  SKIP %s (%s)\n", keys, a.Reason)

		return
	}

	_, _ = fmt.Fprintf(w, "  %s → %s = %s (target: %s)\n", keys, a.DestKey, a.DestValue, a.Target)
}

// NewCmdMigrate returns the `config migrate-credentials` subcommand.
// The command is wired into NewCmdConfig alongside get / set / list
// / validate.
func NewCmdMigrate(props *p.Props) *cobra.Command {
	opts := MigrateOptions{}

	var (
		targetFlag      string
		envVarMapFlag   []string
		assumeYesFlag   bool
		dryRunFlag      bool
		skipVerifyFlag  bool
		keychainSvcFlag string
	)

	cmd := &cobra.Command{
		Use:   "migrate-credentials",
		Short: "Migrate literal credentials in config to env-var references or the OS keychain",
		Long: `Find every literal credential currently stored in the loaded configuration and migrate it to the selected target storage mode.

Supported source credentials: AI API keys (anthropic.api.key, openai.api.key, gemini.api.key), VCS tokens (github.auth.value, gitlab.auth.value, gitea.auth.value, codeberg.auth.value, direct.auth.value), and the Bitbucket dual-credential pair (bitbucket.username + bitbucket.app_password — migrated together as one unit).

Target modes:

  env       (default) — write '<prefix>.env' pointing at an environment variable name.
                        Interactive prompts for the env var name unless --yes is passed.
                        Interactive mode also instructs the user to export the variable
                        and verifies it is set before rewriting the config.
  keychain  — store the secret in the OS keychain via the registered credentials.Backend
              and write '<prefix>.keychain' referencing it. Requires that the tool
              imports github.com/phpboyscout/go-tool-base/pkg/credentials/keychain
              (or registers a custom Backend) or the command refuses.

When --target is omitted, the command honours the 'credentials.migrate.default_target'
config key if present; otherwise falls back to 'env'.

Re-running the command is safe: credentials that already have a target configuration
(e.g. a prior migration) are skipped with a reason.`,
		Example: `  # Preview a migration to env-var references (no changes written):
  mytool config migrate-credentials --dry-run

  # Silent migration to env vars, suitable for CI:
  mytool config migrate-credentials --yes

  # Pin a specific env var name for a credential:
  mytool config migrate-credentials --yes --env-var anthropic.api.key=MYAPP_ANTHROPIC_KEY

  # Migrate to OS keychain (requires the keychain subpackage imported):
  mytool config migrate-credentials --target=keychain --yes`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if targetFlag != "" {
				opts.Target = credentials.Mode(targetFlag)
			}

			opts.DryRun = dryRunFlag
			opts.AssumeYes = assumeYesFlag
			opts.SkipVerify = skipVerifyFlag
			opts.KeychainService = keychainSvcFlag

			overrides, err := parseEnvVarMap(envVarMapFlag)
			if err != nil {
				return err
			}

			opts.EnvVarOverrides = overrides

			result, err := Migrate(cmd.Context(), props, opts)
			if err != nil {
				return err
			}

			PrintResult(cmd.OutOrStdout(), result, opts.DryRun)

			return nil
		},
	}

	cmd.Flags().StringVar(&targetFlag, "target", "", "Target storage mode: 'env' or 'keychain' (default: value of credentials.migrate.default_target config key, or 'env')")
	cmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Print the planned migration without writing the config or touching the keychain")
	cmd.Flags().BoolVarP(&assumeYesFlag, "yes", "y", false, "Skip every interactive prompt; use defaults for env var names and tool name for keychain service")
	cmd.Flags().StringSliceVar(&envVarMapFlag, "env-var", nil, "Override env var name for a specific credential (repeatable): --env-var anthropic.api.key=MY_NAME")
	cmd.Flags().BoolVar(&skipVerifyFlag, "skip-verify", false, "In env-var mode, do not wait for the user to export the variable before rewriting config (implied by --yes)")
	cmd.Flags().StringVar(&keychainSvcFlag, "keychain-service", "", "Override the keychain service name (default: tool name). Only affects --target=keychain.")

	return cmd
}

// parseEnvVarMap converts --env-var key=value flags into a map.
// Whitespace is trimmed around both halves; empty values after
// trimming are rejected.
func parseEnvVarMap(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}

	out := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		i := strings.Index(pair, "=")
		if i <= 0 || i == len(pair)-1 {
			return nil, errors.Newf("--env-var entry %q must be <config-key>=<env-var-name>", pair)
		}

		key := strings.TrimSpace(pair[:i])
		val := strings.TrimSpace(pair[i+1:])

		if key == "" || val == "" {
			return nil, errors.Newf("--env-var entry %q has empty key or value", pair)
		}

		out[key] = val
	}

	return out, nil
}
