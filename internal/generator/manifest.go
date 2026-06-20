package generator

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// manifestIndent is the two-space indentation every manifest.yaml is
// serialised with. Centralised so the encoder-based write sites cannot
// drift from one another.
const manifestIndent = 2

// ManifestPathFor returns the canonical .gtb/manifest.yaml path under
// the given project root.
func ManifestPathFor(projectPath string) string {
	return filepath.Join(projectPath, ".gtb", "manifest.yaml")
}

// DecodeManifestFile reads and unmarshals the manifest at the given
// manifest.yaml path from fs. It is the single read/decode helper routed
// through by every manifest-loading site (including the generate-flag
// command in internal/cmd), so the read+unmarshal boilerplate — and its
// error wording — lives in one place.
func DecodeManifestFile(fs afero.Fs, manifestPath string) (*Manifest, error) {
	data, err := afero.ReadFile(fs, manifestPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read manifest")
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, errors.Newf("failed to unmarshal manifest: %w", err)
	}

	return &m, nil
}

// EncodeManifestFile serialises m to the given manifest.yaml path on fs
// using the streaming yaml encoder with the canonical two-space indent.
// It is the single encode helper for the encoder-based write sites
// (writeManifest, persistProjectHashes, the skeleton hash refresh), so
// the Create+SetIndent+Encode boilerplate cannot drift.
func EncodeManifestFile(fs afero.Fs, manifestPath string, m *Manifest) error {
	f, err := fs.Create(manifestPath)
	if err != nil {
		return errors.Newf("failed to open manifest for writing: %w", err)
	}

	defer func() { _ = f.Close() }()

	enc := yaml.NewEncoder(f)
	enc.SetIndent(manifestIndent)

	if err := enc.Encode(m); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

// MarshalManifestFile serialises m with yaml.Marshal and writes it to
// the given manifest.yaml path on fs with the supplied file mode. This
// is the buffered (whole-document) write flavour used by the
// update/scan/query/flag sites; it differs from EncodeManifestFile only
// in that it produces the yaml.Marshal default indentation, preserving
// each site's existing on-disk output.
func MarshalManifestFile(fs afero.Fs, manifestPath string, m *Manifest, mode os.FileMode) error {
	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	if err := afero.WriteFile(fs, manifestPath, updated, mode); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

// decodeManifestFile reads and decodes the manifest from the generator's
// filesystem. Thin instance wrapper over DecodeManifestFile.
func (g *Generator) decodeManifestFile(manifestPath string) (*Manifest, error) {
	return DecodeManifestFile(g.props.FS, manifestPath)
}

// encodeManifestFile is the instance wrapper over EncodeManifestFile.
func (g *Generator) encodeManifestFile(manifestPath string, m *Manifest) error {
	return EncodeManifestFile(g.props.FS, manifestPath, m)
}

// marshalManifestFile is the instance wrapper over MarshalManifestFile.
// Every generator write uses the standard DefaultFileMode.
func (g *Generator) marshalManifestFile(manifestPath string, m *Manifest) error {
	return MarshalManifestFile(g.props.FS, manifestPath, m, os.FileMode(DefaultFileMode))
}

func (g *Generator) loadManifest() (*Manifest, error) {
	manifestPath := ManifestPathFor(g.config.Path)

	g.props.Logger.Debugf("Loading manifest from %s", manifestPath)

	if exists, _ := afero.Exists(g.props.FS, manifestPath); !exists {
		g.props.Logger.Debug("Manifest not found")

		return nil, errors.New("manifest.yaml not found")
	}

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}

	g.props.Logger.Debugf("Manifest loaded: %s (%d commands)", m.Properties.Name, len(m.Commands))

	return m, nil
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
	Name                 string            `yaml:"name"`
	Description          MultilineString   `yaml:"description"`
	LongDescription      MultilineString   `yaml:"long_description,omitempty"`
	Aliases              []string          `yaml:"aliases,omitempty"`
	Hidden               bool              `yaml:"hidden,omitempty"`
	Args                 string            `yaml:"args,omitempty"`
	Hash                 string            `yaml:"hash,omitempty"` // Deprecated: use Hashes
	Hashes               map[string]string `yaml:"hashes,omitempty"`
	WithAssets           bool              `yaml:"with_assets,omitempty"`
	WithInitializer      bool              `yaml:"with_initializer,omitempty"`
	WithConfigValidation bool              `yaml:"with_config_validation,omitempty"`
	// Deprecated: no longer has any effect. Subcommands are always wrapped with
	// middleware now that generated commands register children through the
	// setup.Command wrapper's Register (which chains middleware unconditionally),
	// so this directive is a no-op. It is retained only so existing manifests
	// still unmarshal; `regenerate manifest` no longer re-derives or writes it.
	WrapSubcommandsWithMiddleware bool  `yaml:"wrap_subcommands_with_middleware,omitempty"`
	Protected                     *bool `yaml:"protected,omitempty"`
	// MCPEnabled is the tri-state MCP-exposure decision for this command:
	// nil = inherit (default exposed), true = explicitly exposed, false =
	// excluded from the MCP tool surface. Build-time only; see
	// docs/development/specs/2026-06-19-mcp-command-exposure-gating.md.
	MCPEnabled *bool `yaml:"mcp_enabled,omitempty"`

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

// ManifestSigning holds self-update signature-verification configuration
// for generated tools. It is the manifest representation of the generated
// internal/trustkeys package, the props.Tool.Signing wiring, and the
// generated signing.go enforcement defaults. Signing is disabled by
// default — a project with no signing block scaffolds nothing
// signing-related. See docs/development/specs/2026-06-10-signing-generator-feature.md.
type ManifestSigning struct {
	// Enabled gates all signing scaffolding. Defaults to false; set true
	// by `gtb enable signing` or `gtb generate project --signing`.
	Enabled bool `yaml:"enabled,omitempty"`
	// ExternalKeyEmail derives the WKD URL and enables the external
	// (WKD) trust-anchor leg. Empty leaves verification embedded-only.
	ExternalKeyEmail string `yaml:"external_key_email,omitempty"`
	// RequireSignature fails an update closed when no valid signature is
	// present. Stays false until a signed release has shipped (the N+1
	// rollout); only ever flipped via `gtb enable signing --require-signature`.
	RequireSignature bool `yaml:"require_signature,omitempty"`
	// KeySource selects the trust-anchor source: "embedded", "external"
	// or "both" (the framework default when empty).
	KeySource string `yaml:"key_source,omitempty"`
	// RequireExternalCrosscheck fails closed when the external (WKD)
	// resolver is unreachable, rather than degrading to embedded-only.
	RequireExternalCrosscheck bool `yaml:"require_external_crosscheck,omitempty"`
	// Backend selects the `gtb sign` backend the generated release
	// pipeline signs with (e.g. "aws-kms", "local"). Defaults to
	// "aws-kms" when a key id is recorded. Drives the GoReleaser signs
	// block; backend-specific args (e.g. kms_region) are emitted only for
	// the backends that take them.
	Backend string `yaml:"backend,omitempty"`
	// KeyID is the backend-specific signing key identifier passed to
	// `gtb sign --key-id` (a KMS id/ARN/alias, or a PEM path for the
	// local backend). Recording it is what turns the GoReleaser signs
	// block on; empty leaves the release pipeline untouched.
	KeyID string `yaml:"key_id,omitempty"`
	// KMSRegion is the AWS region for the aws-kms backend
	// (`gtb sign --kms-region`). Defaults to "eu-west-2". Ignored by
	// backends that don't take a region.
	KMSRegion string `yaml:"kms_region,omitempty"`
	// PublicKey is the path to the armored public-key file the signature
	// identifies (`gtb sign --public-key`), relative to the project root.
	// Defaults to the embedded-key convention
	// internal/trustkeys/keys/signing-key-v1.asc.
	PublicKey string `yaml:"public_key,omitempty"`
}

// ManifestCI holds CI-pipeline configuration for generated tools. It is the
// manifest representation of the scaffolded GitLab pipeline's configurable
// inputs. Currently it carries only the phpboyscout/cicd component source so a
// mirrored or self-hosted downstream can repoint the include base; the
// component versions are pinned by a generator constant kept in lockstep with
// the framework (not manifest-driven). See
// docs/development/specs/2026-06-15-generator-gitlab-ci-refresh.md.
type ManifestCI struct {
	// ComponentSource is the include base for the phpboyscout/cicd
	// components in the scaffolded .gitlab-ci.yml. Empty means "use the
	// framework default" (DefaultCICDComponentSource,
	// gitlab.com/phpboyscout/cicd); a mirrored/self-hosted downstream sets
	// this to repoint the include base. Defaulted on render so a manifest
	// with no ci block still produces a complete pipeline.
	ComponentSource string `yaml:"component_source,omitempty"`
}

type ManifestProperties struct {
	Name        string            `yaml:"name"`
	Description MultilineString   `yaml:"description"`
	Features    []ManifestFeature `yaml:"features"`
	EnvPrefix   string            `yaml:"env_prefix,omitempty"`
	// UpdatePolicy is the generated tool's self-update posture baseline
	// (disabled / prompt / enabled). Empty = framework default (disabled).
	UpdatePolicy string `yaml:"update_policy,omitempty"`
	// UpdateCheckInterval is the generated tool's baseline self-update-check
	// throttle as a Go duration string (e.g. "24h"). Empty = framework
	// default (24h).
	UpdateCheckInterval string            `yaml:"update_check_interval,omitempty"`
	Help                ManifestHelp      `yaml:"help,omitempty"`
	Telemetry           ManifestTelemetry `yaml:"telemetry,omitempty"`
	Signing             ManifestSigning   `yaml:"signing,omitempty"`
	CI                  ManifestCI        `yaml:"ci,omitempty"`
	// Templates records the custom template-overlay sources applied to the
	// project, in render (layer) order: embedded base → templates[0] →
	// templates[1] → … (last writer wins for a shared path). Each entry is
	// provenance + pinning only; suppression behaviour lives in the source's
	// own gtb-template.yaml descriptor. See
	// docs/development/specs/2026-06-15-generator-custom-partial-templates.md.
	Templates []TemplateSource `yaml:"templates,omitempty"`
}

// TemplateSourceType discriminates the two custom-template source backends.
type TemplateSourceType string

const (
	// TemplateSourceLocal reads a template overlay directly from a
	// filesystem path. No network, no cache, no SHA pin — a content
	// fingerprint is recorded so regenerate can warn on drift.
	TemplateSourceLocal TemplateSourceType = "local"
	// TemplateSourceGit clones a template overlay from a forge repo (public
	// over https/go-git, private via the configured forge auth) and pins the
	// resolved commit SHA for byte-stable regeneration.
	TemplateSourceGit TemplateSourceType = "git"
)

// TemplateSource is the minimal consumer-manifest record for one custom
// template-overlay source: provenance (where it came from, what ref the
// operator asked for) plus the pin (the resolved commit SHA for git, a
// content fingerprint for local) and the per-source rendered-output hashes.
//
// The *behaviour* of a source (which embedded scaffolds it replaces, which
// data-contract version it targets) lives with the template set in its
// gtb-template.yaml descriptor, never here — the consumer manifest stays
// provenance-only so consuming projects do not repeat the author's intent.
type TemplateSource struct {
	// Name is an optional operator-assigned handle used by
	// `gtb template update/remove <name>`. Defaults to the repo/dir base
	// name when unset.
	Name string `yaml:"name,omitempty"`
	// Type is "git" or "local".
	Type TemplateSourceType `yaml:"type"`
	// Location is the forge repo path (org/repo, nested GitLab groups
	// supported) or a full clone URL for git sources, or a filesystem path
	// for local sources.
	Location string `yaml:"location"`
	// Ref is the branch/tag/commit the operator specified, recorded verbatim
	// (provenance). Empty/"" defaults to the source's default branch.
	Ref string `yaml:"ref,omitempty"`
	// Resolved is the commit SHA Ref resolved to at generate time — the pin
	// regenerate reproduces from. Empty for local sources.
	Resolved string `yaml:"resolved,omitempty"`
	// Fingerprint is a content fingerprint of a local source's tree at
	// generate time, so regenerate can warn when the on-disk source drifts.
	// Empty for git sources (the resolved SHA is the pin).
	Fingerprint string `yaml:"fingerprint,omitempty"`
	// Hashes records each rendered overlay file's SHA256 keyed by output
	// relative path, so a source's footprint is self-contained and can be
	// removed cleanly (D5).
	Hashes map[string]string `yaml:"hashes,omitempty"`
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
