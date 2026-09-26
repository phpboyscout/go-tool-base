package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
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

// EncodeManifestFile serialises m to the given manifest.yaml path on fs at the
// canonical two-space indent and writes it with DefaultFileMode. It is the
// single serialise-and-write helper for every manifest write site (scaffold,
// generate command, regenerate, add-flag, scan), so the render+write boilerplate
// — and the on-disk byte layout — cannot drift between sites.
func EncodeManifestFile(fs afero.Fs, manifestPath string, m *Manifest) error {
	out, err := marshalManifestBytes(m)
	if err != nil {
		return err
	}

	if err := afero.WriteFile(fs, manifestPath, out, os.FileMode(DefaultFileMode)); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

// marshalManifestBytes renders a manifest to its canonical YAML bytes at the
// two-space manifestIndent. It is the SINGLE serialisation point for every
// manifest write site (scaffold, generate command, regenerate manifest) and the
// dry-run preview, so all sites round-trip byte-stably — previously the encoder
// path used 2-space and the yaml.Marshal path used 4-space, so the first
// generate/regenerate reformatted a freshly scaffolded manifest (keryx
// round-trip churn).
func marshalManifestBytes(m *Manifest) ([]byte, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(manifestIndent)

	if err := enc.Encode(m); err != nil {
		_ = enc.Close()

		return nil, errors.Newf("failed to marshal manifest: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, errors.Newf("failed to flush manifest: %w", err)
	}

	return buf.Bytes(), nil
}

// decodeManifestFile reads and decodes the manifest from the generator's
// filesystem. Thin instance wrapper over DecodeManifestFile.
func (g *Generator) decodeManifestFile(manifestPath string) (*Manifest, error) {
	return DecodeManifestFile(g.props.FS, manifestPath)
}

// marshalManifestFile is the single instance write helper: it serialises via
// EncodeManifestFile and keeps the annotated provenance file in sync. It is the
// choke point every generator manifest write passes through, so the
// not-in-source properties (signing, template overlays, module_published) always
// have an on-disk record for a from-scratch rebuild to recover.
func (g *Generator) marshalManifestFile(manifestPath string, m *Manifest) error {
	if err := EncodeManifestFile(g.props.FS, manifestPath, m); err != nil {
		return err
	}

	return g.writeProvenanceFile(projectPathOf(manifestPath), m)
}

// projectPathOf inverts ManifestPathFor.
func projectPathOf(manifestPath string) string {
	return filepath.Dir(filepath.Dir(manifestPath))
}

func (g *Generator) loadManifest() (*Manifest, error) {
	manifestPath := ManifestPathFor(g.config.Path)

	g.props.Logger.Debug("loading manifest", "path", manifestPath)

	if exists, _ := afero.Exists(g.props.FS, manifestPath); !exists {
		g.props.Logger.Debug("Manifest not found")

		return nil, errors.New("manifest.yaml not found")
	}

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return nil, err
	}

	g.props.Logger.Debug("manifest loaded", "name", m.Properties.Name, "commands", len(m.Commands))

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
	Protected            *bool             `yaml:"protected,omitempty"`
	// MCPEnabled is the tri-state MCP-exposure decision for this command:
	// nil = inherit (default exposed), true = explicitly exposed, false =
	// excluded from the MCP tool surface. Build-time only; see
	// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0089-mcp-command-exposure-gating.
	MCPEnabled *bool `yaml:"mcp_enabled,omitempty"`
	// MCPHints are the MCP tool annotations the command declares about itself
	// (spec 0201 D5). Absent means it states nothing; each hint is tri-state.
	MCPHints *ManifestMCPHints `yaml:"mcp_hints,omitempty"`

	PersistentPreRun  bool              `yaml:"persistent_pre_run,omitempty"`
	PreRun            bool              `yaml:"pre_run,omitempty"`
	MutuallyExclusive [][]string        `yaml:"mutually_exclusive,omitempty"`
	RequiredTogether  [][]string        `yaml:"required_together,omitempty"`
	Flags             []ManifestFlag    `yaml:"flags,omitempty"`
	Commands          []ManifestCommand `yaml:"commands,omitempty"`
	Warning           string            `yaml:"-"` // Used for comments
}

// ManifestMCPHints is the manifest spelling of [setup.MCPHints]: the display
// title and the four MCP behavioural hints, rendered into the command's cmd.go
// as one setup.AnnotateMCP call and read back from it.
type ManifestMCPHints struct {
	Title       string `yaml:"title,omitempty"`
	ReadOnly    *bool  `yaml:"read_only,omitempty"`
	Destructive *bool  `yaml:"destructive,omitempty"`
	Idempotent  *bool  `yaml:"idempotent,omitempty"`
	OpenWorld   *bool  `yaml:"open_world,omitempty"`
}

// ManifestMCPHintsFrom returns the manifest block for hints, or nil when they
// state nothing, so an empty block never appears in the manifest.
func ManifestMCPHintsFrom(h setup.MCPHints) *ManifestMCPHints {
	if h.IsZero() {
		return nil
	}

	return &ManifestMCPHints{Title: h.Title, ReadOnly: h.ReadOnly, Destructive: h.Destructive, Idempotent: h.Idempotent, OpenWorld: h.OpenWorld}
}

// Setup converts the block to the setup value; a nil block states nothing.
func (h *ManifestMCPHints) Setup() setup.MCPHints {
	if h == nil {
		return setup.MCPHints{}
	}

	return setup.MCPHints{Title: h.Title, ReadOnly: h.ReadOnly, Destructive: h.Destructive, Idempotent: h.Idempotent, OpenWorld: h.OpenWorld}
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
				// Attach the warning as a line comment on the default value.
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

// ManifestBootstrap holds config-bootstrap lifecycle policy for generated
// tools — the manifest representation of props.Tool.Bootstrap. Empty scaffolds
// nothing (framework default: a missing config is a hard error when init is
// enabled). See https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0114-bootstrap-auto-initialise-skip-config-check.
type ManifestBootstrap struct {
	// AutoInitialise runs a non-interactive init to write the default config
	// when it is missing, instead of failing. Defaults to false.
	AutoInitialise bool `yaml:"auto_initialise,omitempty"`
	// SkipConfigCheck lists commands (by Name() or full CommandPath()) whose
	// missing-config gate is relaxed to a tolerant load so they own bootstrap.
	SkipConfigCheck []string `yaml:"skip_config_check,omitempty"`
	// AuxiliaryCommands names commands that take the root pre-run's
	// auxiliary fast path (props.Tool.AuxiliaryCommands); spec 0197 D4.
	AuxiliaryCommands []string `yaml:"auxiliary_commands,omitempty"`
}

// ManifestSigning holds self-update signature-verification configuration
// for generated tools. It is the manifest representation of the generated
// internal/trustkeys package, the props.Tool.Signing wiring, and the
// generated signing.go enforcement defaults. Signing is disabled by
// default — a project with no signing block scaffolds nothing
// signing-related. See https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0071-signing-generator-feature.
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
	// RequireChecksum is the author's baseline for checksum enforcement on
	// self-update downloads (props.Tool.RequireChecksum); spec 0197 D4.
	RequireChecksum bool `yaml:"require_checksum,omitempty"`
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
// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0082-generator-gitlab-ci-refresh.
type ManifestCI struct {
	// ComponentSource is the include base for the phpboyscout/cicd
	// components in the scaffolded .gitlab-ci.yml. Empty means "use the
	// framework default" (DefaultCICDComponentSource,
	// gitlab.com/phpboyscout/cicd); a mirrored/self-hosted downstream sets
	// this to repoint the include base. Defaulted on render so a manifest
	// with no ci block still produces a complete pipeline.
	ComponentSource string `yaml:"component_source,omitempty"`
}

// ManifestChat is the manifest's chat block: which providers the tool links.
// An absent block (nil Providers) is a project generated before the block
// existed and reads as the default set on regenerate (spec 0194 D7); an
// explicit empty list means "ai enabled, link no provider" and is kept as
// written (#45). The two must marshal differently, so the list carries no
// omitempty and IsZero decides whether the block is emitted at all.
type ManifestChat struct {
	Providers []string            `yaml:"providers"`
	Default   ManifestChatDefault `yaml:"default,omitempty"`
}

// ManifestChatDefault is the author's default for the tool's chat client: the
// provider, optionally its model, and the addressing a few providers refuse
// to construct without. It is rendered into the tool's embedded defaults
// (cmd/<name>/chat/assets) so the running tool reads it as its lowest config
// layer, and the end user overrides it in their own file (spec 0196 D3, D4).
// Credentials never appear here.
type ManifestChatDefault struct {
	Provider   string `yaml:"provider,omitempty"`
	Model      string `yaml:"model,omitempty"`
	BaseURL    string `yaml:"base_url,omitempty"`
	APIVersion string `yaml:"api_version,omitempty"`
	Project    string `yaml:"project,omitempty"`
	Location   string `yaml:"location,omitempty"`
}

// IsZero reports a default the author has not stated; yaml omits the block.
func (d ManifestChatDefault) IsZero() bool { return d == ManifestChatDefault{} }

// IsZero reports an absent block, which yaml omits; an empty non-nil list is
// not zero and is written as `providers: []`.
func (c ManifestChat) IsZero() bool { return c.Providers == nil && c.Default.IsZero() }

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
	// Chat records the chat providers the tool ships, each a blank import of
	// the module that registers it in cmd/<name>/chat.go. A project generated
	// before this field existed has no block; regenerate reads that as every
	// provider the framework could configure at the time and writes the list
	// out explicitly (spec 0194 D5, D7).
	Chat      ManifestChat      `yaml:"chat,omitempty"`
	Bootstrap ManifestBootstrap `yaml:"bootstrap,omitempty"`
	CI        ManifestCI        `yaml:"ci,omitempty"`
	// Templates records the custom template-overlay sources applied to the
	// project, in render (layer) order: embedded base → templates[0] →
	// templates[1] → … (last writer wins for a shared path). Each entry is
	// provenance + pinning only; suppression behaviour lives in the source's
	// own gtb-template.yaml descriptor. See
	// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0080-generator-custom-partial-templates.
	Templates []TemplateSource `yaml:"templates,omitempty"`
	// ModulePath is the Go module path (spec 0195 D5). A hosted project
	// derives <host>/<org>/<repo>; one that is not names its own. Absent on a
	// manifest that predates the field; regenerate derives and records it.
	ModulePath string `yaml:"module_path,omitempty"`
	// ForgeCredentials are forges enabled for their credential wizard and
	// adapter only, never the release source (spec 0195 D6).
	ForgeCredentials []props.FeatureID `yaml:"forge_credentials,omitempty"`
	// Config declares the project's configuration stack (spec 0204). It is
	// declared here rather than hand-wired in the scaffolded main because the
	// manifest reconstructs byte-exactly from scratch: a hand-wired stack would
	// be a hole reconstruction cannot fill (spec 0183 D8).
	Config ManifestConfig `yaml:"config,omitempty"`
	// LegacyConfigLayers is spec 0183's config_layers, read so the first
	// regenerate can move it into Config.Layers (spec 0204 D13) and never
	// written.
	LegacyConfigLayers []string `yaml:"config_layers,omitempty"`
	// MCP is the tool's MCP publication mode (spec 0201 D3): "direct" publishes
	// one native tool per command; absent or "compact" publishes the three
	// discovery tools. Rendered into props.Tool.MCP; the binary never reads it.
	MCP ManifestMCP `yaml:"mcp,omitempty"`
	// DocsLayout records the documentation tree layout: [DocsLayoutDiataxis]
	// (the default for newly generated projects) or [DocsLayoutFlat] (the legacy
	// docs/commands + docs/packages tree). Empty is treated as flat for backward
	// compatibility with projects generated before this field existed.
	DocsLayout string `yaml:"docs_layout,omitempty"`
	// ModulePublished indicates the module is publicly published (e.g. on
	// pkg.go.dev), allowing generated explanation docs to defer the package API
	// reference there. Default false: the API reference is stubbed locally, since
	// an unpublished module has no pkg.go.dev page to link.
	ModulePublished bool `yaml:"module_published,omitempty"`
	// ExternalCommands declares external Cobra command trees attached to the
	// generated project's root via the declarative channel — the manifest
	// records the module pin + the call descriptors, and the generator renders
	// the attach calls into pkg/cmd/root/cmd.go on every root render (so they
	// survive regenerate / enable / disable). See
	// https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0182-external-command-attachment.
	ExternalCommands []ManifestExternalCommand `yaml:"external_commands,omitempty"`
	// ExternalCommandsAdapter, when true, wires the user-owned adapter escape
	// hatch (pkg/cmd/external/attach.go, exposing external.Commands(p)) into the
	// generated root. The adapter file is scaffolded once and thereafter
	// author-owned; this flag only records that the root must emit the call.
	ExternalCommandsAdapter bool `yaml:"external_commands_adapter,omitempty"`
}

// ManifestConfig is the project's declared configuration stack.
type ManifestConfig struct {
	// Layers is the stack by name, lowest precedence first (see
	// props.ConfigLayer). Empty means the project states nothing and inherits
	// the framework default.
	Layers []string `yaml:"layers,omitempty"`
	// Formats are the config formats the tool links beyond YAML, which is
	// built in and never listed (spec 0204 D2). Each is a blank import in
	// cmd/<name>/config.go.
	Formats []string `yaml:"formats,omitempty"`
	// Format is the tool's own config format; empty is YAML.
	Format string `yaml:"format,omitempty"`
}

// ManifestMCP is the properties.mcp block.
type ManifestMCP struct {
	Mode string `yaml:"mode,omitempty"`
}

// Documentation tree layouts recorded in [ManifestProperties.DocsLayout].
const (
	// DocsLayoutDiataxis is the Diátaxis-structured docs tree (how-to /
	// reference / explanation / tutorials). The default for new projects.
	DocsLayoutDiataxis = "diataxis"
	// DocsLayoutFlat is the legacy flat tree (docs/commands + docs/packages).
	DocsLayoutFlat = "flat"
)

// ResolvedDocsLayout returns the effective documentation layout, defaulting an
// empty or unrecognised value to [DocsLayoutFlat] for backward compatibility
// with projects generated before the docs_layout field existed.
func (p ManifestProperties) ResolvedDocsLayout() string {
	if p.DocsLayout == DocsLayoutDiataxis {
		return DocsLayoutDiataxis
	}

	return DocsLayoutFlat
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

// ManifestExternalCommand declares one external module whose Cobra command
// builders are attached to the generated project's root (the declarative
// channel). It carries a provenance pin (module + version) and the call
// descriptors the generator needs to render each attachment, and holds no
// behaviour — mirroring [TemplateSource].
type ManifestExternalCommand struct {
	// Module is the Go module path providing the commands, e.g.
	// "gitlab.com/phpboyscout/go/signing-cli". Used for the go.mod require.
	Module string `yaml:"module"`
	// Version is the module version to require, e.g. "v0.1.0". Required — an
	// explicit pin; there is no implicit latest resolution.
	Version string `yaml:"version"`
	// ImportPath is the package to import for the constructors. Defaults to
	// Module when empty (the signing-cli case: the constructors live in the
	// module root package).
	ImportPath string `yaml:"import_path,omitempty"`
	// Alias is the import alias for ImportPath in the generated root. Defaults
	// to the import path's base name when empty.
	Alias string `yaml:"alias,omitempty"`
	// Attach lists the constructor calls to render onto the root. At least one
	// entry is required — a module with nothing to attach is meaningless.
	Attach []ManifestExternalAttach `yaml:"attach"`
}

// ManifestExternalAttach describes a single external constructor call to render
// onto the generated root.
type ManifestExternalAttach struct {
	// Constructor is the exported symbol to call, e.g. "NewCmdSign".
	Constructor string `yaml:"constructor"`
	// Args are injection tokens from the closed vocabulary
	// ([templates.ExternalArgTokens]), rendered in order. Empty means a
	// zero-argument constructor.
	Args []string `yaml:"args,omitempty"`
	// Wrap is true when Constructor returns *cobra.Command and must be wrapped
	// via setup.Wrap("", …); false when it returns *setup.Command and is
	// attached directly. It describes the constructor's return type, not gating
	// — declarative attachments are un-gated (always-on) in v1.
	Wrap bool `yaml:"wrap"`
	// Name, if set, is the expected top-level command name, used only for
	// best-effort collision detection. It does not affect the rendered call.
	Name string `yaml:"name,omitempty"`
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
	Type string `yaml:"type"`
	// Backend is the forge the project is hosted on (spec 0195 D2). Absent on
	// a manifest that predates the field; regenerate derives and records it.
	Backend props.FeatureID `yaml:"backend,omitempty"`
	Host    string          `yaml:"host"`
	Owner   string          `yaml:"owner"`
	Repo    string          `yaml:"repo"`
	Private bool            `yaml:"private,omitempty"`
	// Static is the static release channel's one setting when Type is static
	// (spec 0203 D1).
	Static ManifestStaticSource `yaml:"static,omitempty"`
	// Direct is the withdrawn direct channel's block (spec 0195 D7). It is
	// kept so an older manifest loads; nothing reads it and regenerate drops
	// it (spec 0203 D9).
	Direct ManifestDirectSource `yaml:"direct,omitempty"`
}

// ManifestStaticSource is release_source.static: the base URL the pointer and
// the per-tag manifests are published under.
type ManifestStaticSource struct {
	BaseURL string `yaml:"base_url,omitempty"`
}

// ManifestDirectSource mirrors the keys the withdrawn direct channel recorded.
type ManifestDirectSource struct {
	URLTemplate          string `yaml:"url_template,omitempty"`
	ChecksumURLTemplate  string `yaml:"checksum_url_template,omitempty"`
	SignatureURLTemplate string `yaml:"signature_url_template,omitempty"`
	VersionURL           string `yaml:"version_url,omitempty"`
	VersionFormat        string `yaml:"version_format,omitempty"`
	VersionKey           string `yaml:"version_key,omitempty"`
	PinnedVersion        string `yaml:"pinned_version,omitempty"`
}

type ManifestVersion struct {
	GoToolBase string `yaml:"gtb"`
	// Go is the go directive the project was generated with, so regenerate
	// rewrites the go line only when this changes and never because the
	// author's toolchain moved (spec 0197 D3). Absent in older manifests.
	Go string `yaml:"go,omitempty"`
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
