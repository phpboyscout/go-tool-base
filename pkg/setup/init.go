package setup

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/spf13/viper"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
)

const (
	// dirPermUserOnly is the permission mode for user-only directories (0700).
	dirPermUserOnly = 0o700
	// dirPermStandard is the standard permission mode for directories (0755).
	dirPermStandard = 0o755
)

// Initialiser is an optional config step that can check if it's

const (
	DefaultConfigFilename = "config.yaml"
)

//go:embed assets/config.yaml
var DefaultConfig []byte

// Initialiser is an optional config step that can check if it's
// already configured and, if not, interactively populate the shared viper config.
type Initialiser interface {
	// Name returns a human-readable name for logging.
	Name() string
	// IsConfigured returns true if this initialiser's config is already present.
	IsConfigured(cfg config.Containable) bool
	// Configure runs the interactive config and writes values into cfg.
	Configure(p *props.Props, cfg config.Containable) error
}

// InitOptions holds the options for the Initialise function.
type InitOptions struct {
	Dir          string
	Clean        bool
	SkipLogin    bool
	SkipKey      bool
	SkipAI       bool
	Initialisers []Initialiser
}

func GetDefaultConfigDir(fs afero.Fs, name string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	defaultConfigDir := filepath.Join(homeDir, fmt.Sprintf(".%s", strings.ToLower(name)))

	_ = fs.MkdirAll(defaultConfigDir, dirPermUserOnly)

	return defaultConfigDir
}

// Initialise creates the default configuration file in the specified directory.
func Initialise(props *props.Props, opts InitOptions) (string, error) {
	targetFile := filepath.Join(opts.Dir, DefaultConfigFilename)

	if err := props.FS.MkdirAll(opts.Dir, dirPermStandard); err != nil {
		return "", errors.Newf("Failed to create directory: %s", err)
	}

	cfg, err := initializeConfig(props, opts.Dir, targetFile, opts.Clean)
	if err != nil {
		return targetFile, err
	}

	// Initialisers are now the primary way to configure addition steps like GitHub or AI

	// Run any additional initialisers
	c := config.NewContainerFromViper(nil, cfg)
	for _, init := range opts.Initialisers {
		if init.IsConfigured(c) {
			props.Logger.Infof("%s is already configured", init.Name())

			continue
		}

		props.Logger.Infof("%s is enabled but not yet configured", init.Name())

		if err := init.Configure(props, c); err != nil {
			props.Logger.Warnf("%s configuration skipped: %s", init.Name(), err)
		}
	}

	if err := writeGitignore(props.FS, opts.Dir); err != nil {
		props.Logger.Warnf("failed to write .gitignore: %s", err)
	}

	warnIfAPIKeysInGitRepo(props, opts.Dir)

	return targetFile, cfg.WriteConfigAs(targetFile)
}

func mergeExtraConfig(props *props.Props, cfg *viper.Viper) error {
	if props.Assets == nil {
		return nil
	}

	f, err := props.Assets.Open(filepath.Join("assets/init", DefaultConfigFilename))
	if err != nil {
		return nil
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil || len(data) == 0 {
		return nil
	}

	if err := cfg.MergeConfig(bytes.NewReader(data)); err != nil {
		return errors.Newf("Failed to merge extra configuration: %s", err)
	}

	return nil
}

func initializeConfig(props *props.Props, dir, targetFile string, clean bool) (*viper.Viper, error) {
	cfg := viper.New()
	cfg.SetFs(props.FS) // Use Afero FS in Viper
	cfg.SetConfigType("yaml")

	if err := cfg.ReadConfig(bytes.NewReader(DefaultConfig)); err != nil {
		return nil, errors.Newf("Failed to read default configuration: %s", err)
	}

	// Load and merge any domain-specific "extra" configs from the Assets layer.
	if err := mergeExtraConfig(props, cfg); err != nil {
		return nil, err
	}

	if _, err := props.FS.Stat(targetFile); err == nil && !clean {
		props.Logger.Info("Configuration file already exists, attempting to merge")
		cfg.AddConfigPath(dir)

		if err = cfg.MergeInConfig(); err != nil {
			return nil, errors.Newf("Failed to merge configuration: %s", err)
		}
	}

	return cfg, nil
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
				p.Logger.Warnf("config file may contain API keys — ensure it is gitignored: %s", path)

				return filepath.SkipDir
			}
		}

		return nil
	})
}

// configureSSHKeyConfig holds the form options for SSH key configuration.
