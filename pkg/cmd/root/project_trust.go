package root

import (
	"sort"
	"strings"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// protectedProjectConfigKeys are the security-sensitive configuration paths that
// an UNTRUSTED project-local ".<tool>.yaml" is not allowed to set. A cloned
// repository must not be able to silently downgrade self-update verification or
// flip telemetry consent simply by shipping one of these keys — so they are
// stripped from the project-local layer until the directory is trusted via
// `<tool> config trust`. Credentials are handled separately by the "auth"/"api"
// segment rule in isProtectedProjectKey; these are the exact scalar/section
// paths reachable by the update and telemetry resolution chains
// (pkg/setup/update.go, buildTelemetryCollector).
var protectedProjectConfigKeys = []string{
	"update.require_signature",
	"update.require_checksum",
	"update.require_external_crosscheck",
	"update.policy",
	"update.key_source",
	"update.external_key_email",
	"telemetry.enabled",
	"telemetry.consent",
	// Credential subtrees carried as literal API keys.
	"anthropic.api",
	"openai.api",
	"gemini.api",
	"bitbucket.app_password",
}

// isProtectedProjectKey reports whether the dotted path full (whose final
// segment is segment) is one an untrusted project-local layer must not set.
// Any segment named "auth" is protected, which covers every provider's
// credential subtree (github.auth.value, gitlab.auth.env, …) without enrolling
// each provider by name.
func isProtectedProjectKey(full, segment string) bool {
	if segment == "auth" {
		return true
	}

	for _, key := range protectedProjectConfigKeys {
		if full == key {
			return true
		}
	}

	return false
}

// stripProtectedKeys removes every security-sensitive key from a decoded config
// document in place and returns the dotted paths it removed, sorted for stable
// reporting. Nested maps are walked so a key set at any depth is caught.
func stripProtectedKeys(doc map[string]any) []string {
	var removed []string

	stripInto(doc, "", &removed)
	sort.Strings(removed)

	return removed
}

func stripInto(m map[string]any, prefix string, removed *[]string) {
	for k, v := range m {
		full := k
		if prefix != "" {
			full = prefix + "." + k
		}

		if isProtectedProjectKey(full, k) {
			delete(m, k)

			*removed = append(*removed, full)

			continue
		}

		switch child := v.(type) {
		case map[string]any:
			stripInto(child, full, removed)
		case map[any]any:
			// yaml.v3 can decode nested maps with non-string keys; normalise
			// the string-keyed subset so protected keys there are still caught.
			norm := make(map[string]any, len(child))
			for ck, cv := range child {
				if ks, ok := ck.(string); ok {
					norm[ks] = cv
				}
			}

			stripInto(norm, full, removed)
		}
	}
}

// trustFilterCodec is a read-only config codec that decodes YAML exactly like
// the core YAMLCodec but strips the security-sensitive keys an untrusted
// project-local layer is not permitted to set, warning once per decode about
// what it ignored. It deliberately does NOT implement EditingCodec, so the
// backend built from it is not a write target: writes to a project-local file
// that the user has not trusted route to the user's own config instead of the
// repository file.
type trustFilterCodec struct {
	base config.YAMLCodec
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
func projectLayerBackend(props *p.Props, fsys config.FS, projectPath string) config.Backend {
	trusted, err := setup.IsProjectConfigTrusted(props.FS, props.Tool.Name, projectPath)
	if err != nil {
		props.Logger.Debug("project config trust check failed; treating as untrusted", "error", err)
	}

	if trusted {
		props.Logger.Debug("project config layer trusted", "file", projectPath)

		return config.NewFileBackend(fsys, projectPath)
	}

	return config.NewCodecBackend(fsys, projectPath, trustFilterCodec{
		log:  props.Logger,
		tool: props.Tool.Name,
	})
}
