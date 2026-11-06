package root

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// protectedProjectConfigKeys are the security-sensitive configuration paths that
// an UNTRUSTED project-local ".<tool>.yaml" is not allowed to set. A cloned
// repository must not be able to silently downgrade self-update verification or
// flip telemetry consent simply by shipping one of these keys — so they are
// stripped from the project-local layer until the directory is trusted via
// `<tool> config trust`. Credentials are handled separately by the "auth"
// segment rule in isProtectedProjectKey; these are the exact scalar/section
// paths reachable by the update and telemetry resolution chains
// (pkg/setup/update.go, buildTelemetryCollector).
var protectedProjectConfigKeys = []string{
	setup.ConfigKeyUpdateRequireSignature,
	setup.ConfigKeyUpdateRequireChecksum,
	setup.ConfigKeyUpdateRequireExternalCrosscheck,
	setup.ConfigKeyUpdatePolicy,
	setup.ConfigKeyUpdateKeySource,
	setup.ConfigKeyUpdateExternalKeyEmail,
	setup.ConfigKeyTelemetryEnabled,
	setup.ConfigKeyTelemetryConsent,
}

// protectedProjectKeys is protectedProjectConfigKeys plus the credential
// subtrees carried as literal API keys, read from the packages that declare
// them.
func protectedProjectKeys() []string {
	keys := slices.Clone(protectedProjectConfigKeys)
	for _, k := range chat.ProviderCredentialKeys() {
		keys = append(keys, k.Root)
	}

	return append(keys, credentialposture.DualCredential("bitbucket").Password)
}

// isProtectedProjectKey reports whether the dotted path full is one an untrusted
// project-local layer must not set. Any segment named "auth" is protected, which
// covers every provider's credential subtree (github.auth.value, gitlab.auth.env,
// …) without enrolling each provider by name.
//
// The comparison folds as the store does, because the store folds a file's keys
// only after this filter has seen them. It folds in exact mode too: a tool there
// reads one spelling, and any other it loses is warned about and config trust
// restores it.
func isProtectedProjectKey(full string) bool {
	folded := strings.ToLower(full)

	if folded == configSourcesKey || slices.Contains(strings.Split(folded, "."), "auth") {
		return true
	}

	return slices.ContainsFunc(protectedProjectKeys(), func(key string) bool {
		return folded == strings.ToLower(key)
	})
}

// stripProtectedKeys removes every security-sensitive key from a decoded config
// document in place and returns the dotted paths it removed, sorted for stable
// reporting.
func stripProtectedKeys(doc map[string]any) []string {
	return stripKeys(doc, isProtectedProjectKey)
}

// stripKeys removes the keys matching protected from every map in a decoded
// document, and returns their dotted paths as the file spells them, sorted.
func stripKeys(doc map[string]any, protected func(full string) bool) []string {
	var removed []string

	stripValue(doc, "", protected, &removed)
	sort.Strings(removed)

	return removed
}

// stripValue walks each container a codec can produce. yaml.v3 yields map[any]any
// for a map with any non-string key, and the store accepts that form, so it is
// stripped in place. Keys render as the store's own stringKey renders them.
func stripValue(v any, prefix string, protected func(string) bool, removed *[]string) {
	switch node := v.(type) {
	case map[string]any:
		for k, child := range node {
			if stripEntry(k, child, prefix, protected, removed) {
				delete(node, k)
			}
		}
	case map[any]any:
		for k, child := range node {
			if stripEntry(fmt.Sprint(k), child, prefix, protected, removed) {
				delete(node, k)
			}
		}
	case []any:
		for _, item := range node {
			stripValue(item, prefix, protected, removed)
		}
	}
}

// stripEntry reports whether the key should be deleted, recording it if so and
// walking its value if not.
func stripEntry(key string, value any, prefix string, protected func(string) bool, removed *[]string) bool {
	full := key
	if prefix != "" {
		full = prefix + "." + key
	}

	if protected(full) {
		*removed = append(*removed, full)

		return true
	}

	stripValue(value, full, protected, removed)

	return false
}

// trustFilterCodec is a read-only config codec that decodes exactly like the
// codec the file's extension chose (spec 0204 D17) but strips the security-sensitive keys an untrusted
// project-local layer is not permitted to set, warning once per decode about
// what it ignored. It deliberately does NOT implement EditingCodec, so the
// backend built from it is not a write target: writes to a project-local file
// that the user has not trusted route to the user's own config instead of the
// repository file.
type trustFilterCodec struct {
	base config.Codec
	log  logger.Logger
	tool string
}

// Decode implements config.Codec.
func (c trustFilterCodec) Decode(path string, src []byte) ([]map[string]any, error) {
	docs, err := c.base.Decode(path, src)
	if err != nil {
		return docs, err
	}

	var ignored []string

	for _, doc := range docs {
		if doc == nil {
			continue
		}

		ignored = append(ignored, stripProtectedKeys(doc)...)
	}

	if len(ignored) > 0 && c.log != nil {
		sort.Strings(ignored)
		c.log.Warn(
			"ignoring security-sensitive keys from an untrusted project-local config file",
			"file", path,
			"keys", strings.Join(ignored, ", "),
			"hint", "run '"+c.tool+" config trust' to trust this file if you authored it",
		)
	}

	return docs, nil
}

// projectLayerBackend builds the store backend for a discovered project-local
// config file, choosing between full trust and the stripped, read-only view
// based on the per-user trust store. A trusted file behaves exactly as a normal
// highest-precedence writable YAML layer; an untrusted one is decoded through
// trustFilterCodec so its security-sensitive keys are ignored.
func projectLayerBackend(props *p.Props, fsys config.FS, projectPath string, codec config.Codec) config.Backend {
	trusted, err := setup.IsProjectConfigTrusted(props.FS, props.Tool.Name, projectPath)
	if err != nil {
		props.Logger.Debug("project config trust check failed; treating as untrusted", "error", err)
	}

	if trusted {
		props.Logger.Debug("project config layer trusted", "file", projectPath)

		return config.NewCodecBackend(fsys, projectPath, withoutSourcePointers(codec, props.Logger, projectPath))
	}

	return config.NewCodecBackend(fsys, projectPath, trustFilterCodec{
		base: codec,
		log:  props.Logger,
		tool: props.Tool.Name,
	})
}

// configSourcesKey is the subtree that says where configuration comes from
// (spec 0204 D5). A project file may never set it, trusted or not.
const configSourcesKey = "config.sources"

// stripSourcePointers removes config.sources from a decoded document and
// reports it when it was there.
func stripSourcePointers(doc map[string]any) []string {
	return stripKeys(doc, func(full string) bool {
		return strings.ToLower(full) == configSourcesKey
	})
}

// withoutSourcePointers wraps a trusted project file's codec so it drops
// config.sources and nothing else: config trust admits every other key, but
// trusting a repository to choose where configuration comes from is a larger
// act than trusting its settings. An editing codec stays one, so the trusted
// file is still a write target.
func withoutSourcePointers(codec config.Codec, log logger.Logger, path string) config.Codec {
	filter := sourcePointerFilter{base: codec, log: log, path: path}

	if editing, ok := codec.(config.EditingCodec); ok {
		return editingSourcePointerFilter{EditingCodec: editing, filter: filter}
	}

	return filter
}

type sourcePointerFilter struct {
	base config.Codec
	log  logger.Logger
	path string
}

// Decode implements config.Codec.
func (f sourcePointerFilter) Decode(path string, src []byte) ([]map[string]any, error) {
	docs, err := f.base.Decode(path, src)
	if err != nil {
		return docs, err
	}

	for _, doc := range docs {
		if doc != nil && len(stripSourcePointers(doc)) > 0 && f.log != nil {
			f.log.Warn("ignoring config.sources from a project-local config file; trust does not admit where configuration comes from",
				"file", f.path)
		}
	}

	return docs, nil
}

type editingSourcePointerFilter struct {
	config.EditingCodec
	filter sourcePointerFilter
}

// Decode implements config.Codec.
func (f editingSourcePointerFilter) Decode(path string, src []byte) ([]map[string]any, error) {
	return f.filter.Decode(path, src)
}
