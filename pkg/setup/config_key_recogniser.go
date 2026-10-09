package setup

import (
	"strings"

	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/flags"
)

// ConfigKeyRecogniser answers whether a config key is one the framework or
// the tool owns, at section grain: config validate does not call such a key
// unknown, and doctor does not call a variable that set one stray (spec 0205
// D3).
type ConfigKeyRecogniser struct {
	declared map[string]bool
	sections map[string]bool
}

// NewConfigKeyRecogniser builds the recogniser for p's embedded assets and
// resolved features.
func NewConfigKeyRecogniser(p *props.Props) ConfigKeyRecogniser {
	return ConfigKeyRecogniser{declared: toolDeclaredKeys(p), sections: frameworkSections(p.GetFeatures())}
}

// Recognises reports whether key's top-level section is a framework section
// or a declared credential root, or the tool declares key or an ancestor.
func (r ConfigKeyRecogniser) Recognises(key string) bool {
	return recognisedConfigKey(key, r.declared, r.sections)
}

// fixedFrameworkSections are the top-level config sections the framework and
// its built-in features own that no registry declares. A key beneath one of
// these is a recognised configuration key, not a typo the base schema simply
// does not enumerate: the schema cannot list every feature and resilience key
// without duplicating each one as a struct-tag literal.
var fixedFrameworkSections = []string{
	"log", "update", "server", "telemetry", "ai", "chat", "output", "debug", "ci",
}

// frameworkSections is the set of top-level sections the framework owns: the
// fixed ones, the root of every credential the tool's enabled features
// declare (the forges, the chat providers, whatever a tool registers), and
// the dynamic feature flags' root. It used to be a hand list, and the hand list lacked azure
// (spec 0196 D6) and features (spec 0199), so config validate warned about
// keys the framework reads (F15).
func frameworkSections(set features.Set) map[string]bool {
	sections := make(map[string]bool, len(fixedFrameworkSections))
	for _, name := range fixedFrameworkSections {
		sections[name] = true
	}

	sections[sectionOf(flags.ConfigKey("any"))] = true

	for _, d := range credentialposture.DeclaredFor(set) {
		for _, key := range []string{d.EnvKey, d.KeychainKey, d.LiteralKey} {
			if root := sectionOf(key); root != "" {
				sections[root] = true
			}
		}
	}

	return sections
}

// sectionOf is the top-level section of a dotted key, or empty for none.
func sectionOf(key string) string {
	if i := strings.IndexByte(key, '.'); i >= 0 {
		return key[:i]
	}

	return key
}

// recognisedConfigKey reports whether key is one the framework or the tool
// legitimately owns: its top-level section is a framework section, or the tool
// declares the key (or an ancestor of it) in its embedded defaults or init
// template.
func recognisedConfigKey(key string, declared, sections map[string]bool) bool {
	if key == "" {
		return false
	}

	section := sectionOf(key)

	return sections[section] || declared[key]
}

// toolDeclaredKeys returns the set of config keys (and their ancestor paths)
// the tool declares across its merged embedded defaults and init template —
// the keys the tool officially supports, so a value under one is not "unknown".
func toolDeclaredKeys(p *props.Props) map[string]bool {
	keys := map[string]bool{}

	for _, path := range []string{DefaultsAssetPath, InitTemplateAssetPath} {
		doc := AssetDocument(p, path)
		if len(doc) == 0 {
			continue
		}

		var m map[string]any
		if err := yaml.Unmarshal(doc, &m); err != nil {
			continue
		}

		flattenConfigKeys(m, "", keys)
	}

	return keys
}

// flattenConfigKeys records every dotted key path in m (leaves and the maps
// above them) into out.
func flattenConfigKeys(m map[string]any, prefix string, out map[string]bool) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		out[path] = true

		if nested, ok := v.(map[string]any); ok {
			flattenConfigKeys(nested, path, out)
		}
	}
}
