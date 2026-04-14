package generator

import (
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
)

func (g *Generator) loadManifest() (*Manifest, error) {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	g.props.Logger.Debugf("Loading manifest from %s", manifestPath)

	if exists, _ := afero.Exists(g.props.FS, manifestPath); !exists {
		g.props.Logger.Debug("Manifest not found")

		return nil, errors.New("manifest.yaml not found")
	}

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read manifest")
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, errors.Newf("failed to unmarshal manifest: %w", err)
	}

	g.props.Logger.Debugf("Manifest loaded: %s (%d commands)", m.Properties.Name, len(m.Commands))

	return &m, nil
}

type MultilineString string

func (s MultilineString) MarshalYAML() (any, error) {
	node := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: string(s),
	}
	if strings.Contains(string(s), "\n") {
		node.Style = yaml.LiteralStyle
	}

	return node, nil
}

type Manifest struct {
	Properties    ManifestProperties    `yaml:"properties"`
	ReleaseSource ManifestReleaseSource `yaml:"release_source"`
	Version       ManifestVersion       `yaml:"version"`
	Hashes        map[string]string     `yaml:"hashes,omitempty"` // project-level file hashes (relative path → SHA256)
	Commands      []ManifestCommand     `yaml:"commands,omitempty"`
}

type ManifestCommand struct {
	Name                          string            `yaml:"name"`
	Description                   MultilineString   `yaml:"description"`
	LongDescription               MultilineString   `yaml:"long_description,omitempty"`
	Aliases                       []string          `yaml:"aliases,omitempty"`
	Hidden                        bool              `yaml:"hidden,omitempty"`
	Args                          string            `yaml:"args,omitempty"`
	Hash                          string            `yaml:"hash,omitempty"` // Deprecated: use Hashes
	Hashes                        map[string]string `yaml:"hashes,omitempty"`
	WithAssets                    bool              `yaml:"with_assets,omitempty"`
	WithInitializer               bool              `yaml:"with_initializer,omitempty"`
	WithConfigValidation          bool              `yaml:"with_config_validation,omitempty"`
	WrapSubcommandsWithMiddleware bool              `yaml:"wrap_subcommands_with_middleware,omitempty"`
	Protected                     *bool             `yaml:"protected,omitempty"`

	PersistentPreRun  bool              `yaml:"persistent_pre_run,omitempty"`
	PreRun            bool              `yaml:"pre_run,omitempty"`
	MutuallyExclusive [][]string        `yaml:"mutually_exclusive,omitempty"`
	RequiredTogether  [][]string        `yaml:"required_together,omitempty"`
	Flags             []ManifestFlag    `yaml:"flags,omitempty"`
	Commands          []ManifestCommand `yaml:"commands,omitempty"`
	Warning           string            `yaml:"-"` // Used for comments
}

type ManifestFlag struct {
	Name          string          `yaml:"name"`
	Type          string          `yaml:"type"`
	Description   MultilineString `yaml:"description"`
	Persistent    bool            `yaml:"persistent,omitempty"`
	Shorthand     string          `yaml:"shorthand,omitempty"`
	Default       string          `yaml:"default,omitempty"`
	DefaultIsCode bool            `yaml:"default_is_code,omitempty"`
	Required      bool            `yaml:"required,omitempty"`
	Hidden        bool            `yaml:"hidden,omitempty"`
	Warning       string          `yaml:"-"` // Used for comments
}

func (c ManifestCommand) MarshalYAML() (any, error) {
	type manifestCommandAlias ManifestCommand

	alias := manifestCommandAlias(c)

	// Migration: If we have a single hash but no hashes map, move it to the map
	if alias.Hash != "" {
		if alias.Hashes == nil {
			alias.Hashes = make(map[string]string)
		}

		if _, ok := alias.Hashes["cmd.go"]; !ok {
			alias.Hashes["cmd.go"] = alias.Hash
		}

		alias.Hash = "" // Clear deprecated field
	}

	node := &yaml.Node{}
	if err := node.Encode(alias); err != nil {
		return nil, errors.Wrap(err, "failed to encode manifest command")
	}

	if c.Warning != "" {
		// Set comment on the name value
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "name" {
				node.Content[i+1].LineComment = c.Warning

				break
			}
		}
		// Also set on the node itself just in case
		node.HeadComment = "# " + c.Warning
	}

	return node, nil
}

func (f ManifestFlag) MarshalYAML() (any, error) {
	type manifestFlagAlias ManifestFlag

	node := &yaml.Node{}
	if err := node.Encode(manifestFlagAlias(f)); err != nil {
		return nil, errors.Wrap(err, "failed to encode manifest flag")
	}

	if f.Warning != "" {
		// Find the "default" key in the mapping
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "default" {
				// Add the comment to the value node
				// node.Content[i+1].LineComment = "# " + f.Warning
				// Actually, user wants it "in the manifest... include raw representation"
				// A line comment is perfect.
				node.Content[i+1].LineComment = f.Warning

				break
			}
		}
	}

	return node, nil
}

type ManifestFeature struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}

// ManifestTelemetry holds telemetry configuration for generated tools.
type ManifestTelemetry struct {
	Endpoint     string `yaml:"endpoint,omitempty"`
	OTelEndpoint string `yaml:"otel_endpoint,omitempty"`
}

type ManifestProperties struct {
	Name        string            `yaml:"name"`
	Description MultilineString   `yaml:"description"`
	Features    []ManifestFeature `yaml:"features"`
	EnvPrefix   string            `yaml:"env_prefix,omitempty"`
	Help        ManifestHelp      `yaml:"help,omitempty"`
	Telemetry   ManifestTelemetry `yaml:"telemetry,omitempty"`
}

type ManifestHelp struct {
	Type         string `yaml:"type,omitempty"`
	SlackChannel string `yaml:"slack_channel,omitempty"`
	SlackTeam    string `yaml:"slack_team,omitempty"`
	TeamsChannel string `yaml:"teams_channel,omitempty"`
	TeamsTeam    string `yaml:"teams_team,omitempty"`
}

// GetReleaseSource returns the release source type, owner, and repo.
func (m *Manifest) GetReleaseSource() (sourceType, owner, repo string) {
	return m.ReleaseSource.Type, m.ReleaseSource.Owner, m.ReleaseSource.Repo
}

type ManifestReleaseSource struct {
	Type    string `yaml:"type"`
	Host    string `yaml:"host"`
	Owner   string `yaml:"owner"`
	Repo    string `yaml:"repo"`
	Private bool   `yaml:"private,omitempty"`
}

type ManifestVersion struct {
	GoToolBase string `yaml:"gtb"`
}

func (g *Generator) convertFlagsToManifest(parsedFlags []templates.CommandFlag) []ManifestFlag {
	mFlags := make([]ManifestFlag, 0, len(parsedFlags))

	for _, f := range parsedFlags {
		mFlags = append(mFlags, ManifestFlag{
			Name:          f.Name,
			Type:          f.Type,
			Description:   MultilineString(f.Description),
			Persistent:    f.Persistent,
			Shorthand:     f.Shorthand,
			Default:       f.Default,
			DefaultIsCode: f.DefaultIsCode,
			Required:      f.Required,
			Hidden:        f.Hidden,
		})
	}

	return mFlags
}
