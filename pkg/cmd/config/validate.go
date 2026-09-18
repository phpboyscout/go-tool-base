package config

import (
	"fmt"
	"io"
	"strings"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/flags"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	cfg "gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// NewCmdValidate returns the "config validate" subcommand.
func NewCmdValidate(props *p.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate the current configuration",
		Long: `Check the current configuration against required key definitions.

Reports missing required fields, type mismatches, and unknown keys.
Exits with a non-zero status code if any validation errors are found.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if props.Config == nil {
				return errors.New("no configuration loaded")
			}

			schema, err := buildBaseSchema()
			if err != nil {
				return errors.Wrap(err, "failed to build validation schema")
			}

			view := props.Config.View()
			result := view.Validate(schema)

			// Unknown-key warnings only help when the user can act on them.
			// Keys supplied solely by embedded defaults (the framework or a
			// feature bundle) are not the user's to remove, and keys the
			// framework or the tool legitimately owns are not typos the schema
			// happens not to enumerate — both are filtered. Anything file-,
			// env- or flag-authored and genuinely unrecognised still warns.
			result.Warnings = actionableWarnings(props, view, result.Warnings)

			printValidationResult(cmd.OutOrStdout(), result)

			if !result.Valid() {
				return errors.New("configuration validation failed")
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "configuration is valid")

			return nil
		},
	}

	return cmd
}

// buildBaseSchema returns the minimum schema that every GTB-based tool must satisfy.
func buildBaseSchema() (*cfg.StructSchema, error) {
	type baseConfig struct {
		LogLevel string `config:"log.level" validate:"required" enum:"debug,info,warn,error" description:"log verbosity level"`
	}

	return cfg.NewSchema(cfg.WithStructSchema(baseConfig{}))
}

// unknownKeyMessage is the validation message go/config attaches to a key that
// is not in the schema. Matched to filter those warnings without touching
// value-validation warnings (required, enum, type).
const unknownKeyMessage = "unknown configuration key"

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

// actionableWarnings trims validation warnings to the ones a user can act on.
//
// Two filters. A warning for a key no user-influenced layer defines is dropped:
// keys supplied solely by embedded defaults are not the user's to remove. And
// an unknown-key warning for a key the framework or the tool legitimately owns
// is dropped: the base schema cannot enumerate every valid key, so an unknown
// key is not reliably a typo — only one that matches neither a framework
// section nor a key the tool declares in its own embedded assets survives as a
// genuine "did you mean…". Value-validation warnings (required, enum, type) are
// never dropped by the unknown-key filter.
func actionableWarnings(props *p.Props, view *cfg.View, warnings []cfg.ValidationError) []cfg.ValidationError {
	declared := toolDeclaredKeys(props)
	sections := frameworkSections(props.GetFeatures())
	kept := make([]cfg.ValidationError, 0, len(warnings))

	for _, warning := range warnings {
		if warning.Message == unknownKeyMessage && recognisedConfigKey(warning.Key, declared, sections) {
			continue
		}

		if warning.Key == "" || userAuthoredKey(view, warning.Key) {
			kept = append(kept, warning)
		}
	}

	return kept
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
func toolDeclaredKeys(props *p.Props) map[string]bool {
	keys := map[string]bool{}

	for _, path := range []string{setup.DefaultsAssetPath, setup.InitTemplateAssetPath} {
		doc := setup.AssetDocument(props, path)
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

func printValidationResult(w io.Writer, result *cfg.ValidationResult) {
	for _, e := range result.Errors {
		_, _ = fmt.Fprintf(w, "error:   %s\n", e.String())
	}

	for _, e := range result.Warnings {
		_, _ = fmt.Fprintf(w, "warning: %s\n", e.String())
	}
}
