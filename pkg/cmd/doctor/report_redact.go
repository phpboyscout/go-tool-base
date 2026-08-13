package doctor

import (
	"fmt"
	"strings"

	"gitlab.com/phpboyscout/go/redact"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// redactedSentinel replaces any value whose key is credential-shaped, so even a
// malformed or unexpected value type can never leak.
const redactedSentinel = "<redacted>"

// credentialKeySuffixes are dotted-key endings that always name a secret. They
// mirror the shapes literalCredentialKeys enumerates
// (`*.api.key`, `*.auth.value`, `bitbucket.app_password`).
var credentialKeySuffixes = []string{
	".api.key",
	".auth.value",
	".app_password",
	".password",
	".secret",
	".token",
}

// credentialKeySegments are final key segments that always name a secret,
// regardless of their parent path.
var credentialKeySegments = map[string]struct{}{
	"token":        {},
	"secret":       {},
	"password":     {},
	"app_password": {},
	"apikey":       {},
	"api_key":      {},
}

// isCredentialKey reports whether a dotted config key names a credential, so its
// value must be dropped wholesale (sentinel) rather than value-redacted.
func isCredentialKey(keyPath string) bool {
	lk := strings.ToLower(keyPath)

	// Exact match against every DECLARED credential, so the "literal credential"
	// WARN check and this redaction cannot disagree about a known key — they now
	// read the same inventory rather than two lists someone has to keep in step.
	// It also means a downstream tool's own credential is redacted because the
	// tool declared it, not because it happened to match the generic net below.
	for _, d := range credentialposture.Registered() {
		if d.LiteralKey != "" && lk == strings.ToLower(d.LiteralKey) {
			return true
		}
	}

	for _, suffix := range credentialKeySuffixes {
		if strings.HasSuffix(lk, suffix) {
			return true
		}
	}

	seg := lk
	if i := strings.LastIndex(lk, "."); i >= 0 {
		seg = lk[i+1:]
	}

	if _, ok := credentialKeySegments[seg]; ok {
		return true
	}

	return redact.IsSensitiveHeaderKey(seg)
}

// redactValue walks a resolved-config value depth-first and returns a copy in
// which (a) any leaf under a credential-shaped key becomes the sentinel and
// (b) every other leaf is stringified and scrubbed through redact.String. Map
// keys are preserved. This is the single redaction choke point for the config
// map: no leaf reaches a renderer without passing through here.
func redactValue(keyPath string, v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			child := k
			if keyPath != "" {
				child = keyPath + "." + k
			}

			out[k] = redactValue(child, val)
		}

		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactValue(keyPath, val)
		}

		return out
	default:
		if isCredentialKey(keyPath) {
			return redactedSentinel
		}

		return redact.String(fmt.Sprintf("%v", v))
	}
}

// redactConfig redacts a whole resolved-config map (viper AllSettings).
func redactConfig(settings map[string]any) map[string]any {
	redacted, _ := redactValue("", settings).(map[string]any)

	return redacted
}
