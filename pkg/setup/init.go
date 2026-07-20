package setup

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"dario.cat/mergo"
	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

const (
	// dirPermUserOnly is the permission mode for user-only directories (0700).
	dirPermUserOnly = 0o700
	// dirPermStandard is the standard permission mode for directories (0755).
	dirPermStandard = 0o755
	// configFilePerm restricts the config file — it may contain credentials.
	configFilePerm = 0o600
)

const (
	DefaultConfigFilename = "config.yaml"

	// DefaultsAssetPath is the bare asset path the embedded-defaults layer is
	// read from. props.Assets merges it across every registered bundle, so each
	// package ships its own defaults (segregated-default-config spec, D1).
	DefaultsAssetPath = "assets/config.yaml"

	// InitTemplateAssetPath is the bare asset path init templates are read
	// from — the human-facing document Initialise writes to the user's config
	// file, merged across every registered bundle (D9).
	InitTemplateAssetPath = "assets/init/config.yaml"
)

// Initialiser is an optional config step that can check if it's already
// configured and, if not, interactively populate the tool's config file.
type Initialiser interface {
	// Name returns a human-readable name for logging.
	Name() string
	// IsConfigured returns true if this initialiser's config is already present.
	IsConfigured(cfg config.Reader) bool
	// Configure runs the interactive config and writes values through cfg.
	Configure(p *props.Props, cfg Editor) error
}

// Editor is the read/write surface an initialiser uses during init. Reads
// resolve against the target document layered over the tool's embedded
// defaults; writes go through the store's Apply, which edits the target
// document in place, so template comments survive the wizards.
type Editor interface {
	// View returns a pinned view of the current configuration.
	View() *config.View
	// Set writes one key to the target document.
	Set(key string, value any) error
	// Apply stages several changes as one transactional write to the target
	// document — all land or none do.
	Apply(changes ...config.Change) error
}

type storeEditor struct {
	//nolint:containedctx // scoped to one init run; threading it through every
	// wizard Set call site would churn the whole Initialiser surface for no
	// cancellation gain — the viper-backed flow it replaces had none either.
	ctx   context.Context
	store *config.Store
}

func (e *storeEditor) View() *config.View { return e.store.View() }

func (e *storeEditor) Set(key string, value any) error {
	_, err := e.store.Apply(e.ctx, config.Set(key, value))

	return errors.Wrapf(err, "writing %s", key)
}

func (e *storeEditor) Apply(changes ...config.Change) error {
	_, err := e.store.Apply(e.ctx, changes...)

	return errors.Wrap(err, "applying config changes")
}

// WriteExclusive commits the winning credential keys and removes every other
// key in all, as one transactional Apply — the single-credential-key
// invariant from the hardening spec, enforced atomically so an interrupted
// storage-mode switch cannot leave both the old and new credential behind.
// Removing an already-absent key is a no-op, so stale keys end up absent
// rather than blanked.
//
// Change ordering matters twice over. A stale key on the same dotted path as
// a winning key (bitbucket.username versus bitbucket.username.env) must be
// removed BEFORE that key is set, or the set has to write through a scalar —
// and a later subtree removal would take the winner with it. Every other
// stale key is removed AFTER all sets, so a parent mapping shared by winning
// and stale keys never passes through an empty state mid-batch.
func WriteExclusive(e Editor, winning map[string]any, all []string) error {
	changes := make([]config.Change, 0, len(all)+len(winning))
	removed := make(map[string]bool, len(all))

	for _, key := range slices.Sorted(maps.Keys(winning)) {
		for _, stale := range all {
			if _, wins := winning[stale]; wins || removed[stale] {
				continue
			}

			if samePath(stale, key) {
				changes = append(changes, config.Remove(stale))
				removed[stale] = true
			}
		}

		changes = append(changes, config.Set(key, winning[key]))
	}

	for _, stale := range all {
		if _, wins := winning[stale]; !wins && !removed[stale] {
			changes = append(changes, config.Remove(stale))
		}
	}

	return e.Apply(changes...)
}

// samePath reports whether one dotted key path is an ancestor of the other,
// i.e. writing one would collide with a document node the other occupies.
func samePath(a, b string) bool {
	return strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

// InitOptions holds the options for the Initialise function.
type InitOptions struct {
	Dir          string
	Clean        bool
	Initialisers []Initialiser

	// Interactive overrides terminal detection for the credential wizards.
	// When nil, interactivity is detected from stdin (utils.IsInteractive).
	// Credential initialisers drive interactive prompts that would block on a
	// non-terminal stdin, so they are skipped when this resolves to false.
	// Tests set it explicitly to avoid depending on the test runner's stdin.
	Interactive *bool
}

// interactive resolves the effective interactivity for the credential wizards,
// falling back to live stdin detection when not explicitly overridden.
func (o InitOptions) interactive() bool {
	if o.Interactive != nil {
		return *o.Interactive
	}

	return utils.IsInteractive()
}

// GetDefaultConfigDir returns the default config directory for the named
// tool (~/.toolname/). It returns an empty string when the user home
// directory cannot be resolved (e.g. HOME is unset or empty) — callers
// must treat an empty result as "no config dir" and skip any read/write
// rather than joining it with a filename, which would otherwise resolve to
// a relative path under the current working directory.
//
// It is pure: it computes and returns the path only and never creates the
// directory. Building the command tree (--help, completions, default flag
// values) resolves this path, so a hidden MkdirAll here would create
// ~/.toolname as a side effect of merely running --help. Directory creation is
// deferred to the writers that actually persist a file under it (Initialise,
// setTimeSinceLastIn, the config writers in pkg/cmd), each of which MkdirAlls
// its parent at write time. The fs parameter is retained for API
// compatibility and is unused.
func GetDefaultConfigDir(_ afero.Fs, name string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return ""
	}

	return filepath.Join(homeDir, fmt.Sprintf(".%s", strings.ToLower(name)))
}

// EnsureDefaultConfigDir creates the tool's default config directory with
// user-only permissions (it may hold credential-bearing files) and returns its
// path, or "" when the home directory cannot be resolved. One shared home for
// the pre-create every Apply-to-the-default-path caller needs; each caller
// keeps its own error posture.
func EnsureDefaultConfigDir(fs afero.Fs, name string) (string, error) {
	dir := GetDefaultConfigDir(fs, name)
	if dir == "" {
		return "", nil
	}

	if err := fs.MkdirAll(dir, dirPermUserOnly); err != nil {
		return "", errors.Wrap(err, "failed to create config directory")
	}

	return dir, nil
}

// Initialise creates the tool's configuration file in the specified directory
// and runs the supplied initialisers over it.
func Initialise(ctx context.Context, p *props.Props, opts InitOptions) (string, error) {
	editor, targetFile, err := OpenConfigEditor(ctx, p, opts.Dir, opts.Clean)
	if err != nil {
		return targetFile, err
	}

	interactive := opts.interactive()

	for _, init := range opts.Initialisers {
		if init.IsConfigured(editor.View()) {
			p.Logger.Info("already configured", "component", init.Name())

			continue
		}

		// Credential wizards prompt on stdin; without a terminal they would
		// block indefinitely. Skip them rather than hang — the base config is
		// still written, and the user can run the dedicated "init <provider>"
		// subcommand interactively later.
		if !interactive {
			p.Logger.Info("setup skipped: no interactive terminal", "component", init.Name())

			continue
		}

		p.Logger.Info("enabled but not yet configured", "component", init.Name())

		if err := init.Configure(p, editor); err != nil {
			p.Logger.Warn("configuration skipped", "component", init.Name(), "error", err)
		}
	}

	if err := writeGitignore(p.FS, opts.Dir); err != nil {
		p.Logger.Warn("failed to write .gitignore", "error", err)
	}

	warnIfAPIKeysInGitRepo(p, opts.Dir)

	return targetFile, nil
}

// OpenConfigEditor materialises the tool's config file in dir (seeding it from
// the merged init template when absent or clean, merging new template keys
// under an existing one otherwise) and opens an [Editor] over it. The editor's
// reads resolve the file over the tool's embedded defaults, so a wizard sees
// the same effective values the running tool would; writes land only in the
// file. Shared by Initialise and the per-feature init subcommands.
func OpenConfigEditor(ctx context.Context, p *props.Props, dir string, clean bool) (Editor, string, error) {
	targetFile := filepath.Join(dir, DefaultConfigFilename)

	if err := p.FS.MkdirAll(dir, dirPermStandard); err != nil {
		return nil, targetFile, errors.Wrap(err, "Failed to create directory")
	}

	if err := writeInitialConfig(p, targetFile, clean); err != nil {
		return nil, targetFile, err
	}

	storeOpts := []config.StoreOption{}
	if defaults := AssetSource(p, DefaultsAssetPath); defaults != nil {
		storeOpts = append(storeOpts, config.WithReaders(*defaults))
	}

	storeOpts = append(storeOpts, config.WithFiles(p.GetConfigFS(), targetFile))

	store, err := config.NewStore(ctx, storeOpts...)
	if err != nil {
		return nil, targetFile, errors.Wrap(err, "opening config for initialisation")
	}

	return &storeEditor{ctx: ctx, store: store}, targetFile, nil
}

// AssetDocument returns the named document merged across every registered
// asset bundle, or nil when no bundle ships one.
//
// fs.ReadFile rather than a ReadFile method on Assets, and the distinction is
// load-bearing: fs.ReadFile falls back to Open, and Open is where Assets
// merges a structured file across every registered bundle. A hand-rolled
// ReadFile that indexed one bundle would compile, read plausibly, and
// silently return only the last bundle's copy.
func AssetDocument(p *props.Props, path string) []byte {
	if p.Assets == nil {
		return nil
	}

	data, err := fs.ReadFile(p.Assets, path)
	if err != nil {
		return nil
	}

	return data
}

// AssetSource wraps a merged asset document as a named config source, or nil
// when no bundle ships the path. The "embedded:" prefix is the provenance
// name convention for embedded layers.
func AssetSource(p *props.Props, path string) *config.NamedSource {
	content := AssetDocument(p, path)
	if content == nil {
		return nil
	}

	return &config.NamedSource{Name: "embedded:" + path, Content: content}
}

// writeInitialConfig materialises the target file. Absent (or --clean): the
// merged init template is written. Existing: the file's values are merged over
// the template so re-running init gains new template keys without disturbing
// user values. Both paths re-encode (the cross-bundle template merge parses
// and re-emits, so template comments do not carry) — parity with the viper
// round-trip this replaces. Comments the USER writes survive later edits,
// because wizard writes go through Apply.
func writeInitialConfig(p *props.Props, targetFile string, clean bool) error {
	seed := AssetDocument(p, InitTemplateAssetPath)

	exists, err := afero.Exists(p.FS, targetFile)
	if err != nil {
		return errors.Wrap(err, "checking config file existence")
	}

	if exists && !clean {
		p.Logger.Info("Configuration file already exists, attempting to merge")

		merged, mergeErr := mergeExistingOverTemplate(p.FS, targetFile, seed)
		if mergeErr != nil {
			return mergeErr
		}

		if merged == nil {
			// No template to gain keys from; leave the user's file untouched.
			return nil
		}

		seed = merged
	}

	if err := afero.WriteFile(p.FS, targetFile, seed, configFilePerm); err != nil {
		return errors.Wrap(err, "writing config file")
	}

	// Restrict permissions on pre-existing files — the file may contain
	// credentials, and WriteFile's mode only applies on creation.
	if exists {
		if chmodErr := p.FS.Chmod(targetFile, configFilePerm); chmodErr != nil {
			p.Logger.Warn("failed to set config file permissions", "error", chmodErr)
		}
	}

	return nil
}

// mergeExistingOverTemplate deep-merges the existing file's values over the
// template document, returning the encoded result — or nil when there is no
// template to merge.
func mergeExistingOverTemplate(fsys afero.Fs, targetFile string, seed []byte) ([]byte, error) {
	if len(seed) == 0 {
		return nil, nil
	}

	existing, err := afero.ReadFile(fsys, targetFile)
	if err != nil {
		return nil, errors.Wrap(err, "reading existing config")
	}

	seedDoc := map[string]any{}
	if err := yaml.Unmarshal(seed, &seedDoc); err != nil {
		return nil, errors.Wrap(err, "parsing init template")
	}

	existingDoc := map[string]any{}
	if err := yaml.Unmarshal(existing, &existingDoc); err != nil {
		return nil, errors.Wrap(err, "parsing existing config")
	}

	if err := mergo.Merge(&seedDoc, existingDoc, mergo.WithOverride); err != nil {
		return nil, errors.Wrap(err, "merging existing config over template")
	}

	out, err := yaml.Marshal(seedDoc)
	if err != nil {
		return nil, errors.Wrap(err, "encoding merged config")
	}

	return out, nil
}

const gitignoreContent = `# Ignore files that may contain secrets
*.env
*.secret
*.key
`

// writeGitignore creates a .gitignore in the config directory if one doesn't already exist.
func writeGitignore(fs afero.Fs, configDir string) error {
	gitignorePath := filepath.Join(configDir, ".gitignore")

	exists, err := afero.Exists(fs, gitignorePath)
	if err != nil {
		return errors.Wrap(err, "checking .gitignore existence")
	}

	if exists {
		return nil
	}

	const filePerm = 0o644

	return afero.WriteFile(fs, gitignorePath, []byte(gitignoreContent), filePerm)
}

// warnIfAPIKeysInGitRepo logs a warning if config files in a git repo appear to contain API keys.
func warnIfAPIKeysInGitRepo(p *props.Props, configDir string) {
	// Check if parent directory is a git repo
	_, err := p.FS.Stat(filepath.Join(filepath.Dir(configDir), ".git"))
	if err != nil {
		return // not a git repo
	}

	patterns := []string{"sk-", "api_key", "token", "secret"}

	_ = afero.Walk(p.FS, configDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() {
			return nil
		}

		data, readErr := afero.ReadFile(p.FS, path)
		if readErr != nil {
			return readErr
		}

		content := string(data)
		for _, pattern := range patterns {
			if strings.Contains(content, pattern) {
				p.Logger.Warn("config file may contain API keys — ensure it is gitignored", "path", path)

				return filepath.SkipDir
			}
		}

		return nil
	})
}
