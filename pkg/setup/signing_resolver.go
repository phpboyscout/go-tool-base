package setup

import (
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"

	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// KeyResolverConfig parameterises BuildKeyResolver. It is deliberately
// decoupled from Viper so tool authors and tests can construct it
// directly; SelfUpdater fills it from the resolved update.* config
// keys.
type KeyResolverConfig struct {
	// KeySource is "embedded", "external", or "both". Empty defaults
	// to "both".
	KeySource string

	// ExternalKeyEmail is the release email whose WKD directory holds
	// the external key. Required for "external"; enables the WKD leg of
	// "both" when set.
	ExternalKeyEmail string

	// RequireExternalCrosscheck maps to CompositeResolver.RequireAll:
	// when true, a WKD fetch failure aborts the update instead of
	// falling back to the embedded key.
	RequireExternalCrosscheck bool

	// HTTPClient is the hardened client used for WKD fetches. When nil,
	// a default pkg/http.NewClient() is used.
	HTTPClient *http.Client

	// Logger receives CompositeResolver fail-open warnings. Optional.
	Logger logger.Logger
}

// BuildKeyResolver maps a KeyResolverConfig and a set of embedded
// public keys onto a concrete KeyResolver:
//
//   - "embedded" → EmbeddedResolver(embeddedKeys). Errors without keys.
//   - "external" → WKDResolver(email). Errors without an email.
//   - "both"     → CompositeResolver{Embedded, WKD} when both an email
//     and keys are available; degrades to whichever single source is
//     configured; errors when neither is.
//
// Embedded keys are validated against the minimum-strength policy here
// (via the error-returning constructor) so a malformed or weak key is
// reported as an error rather than panicking mid-update.
//
// Tool authors who want full control can call this directly and pass
// the result via WithKeyResolver; the convenience path is to supply
// raw keys via WithEmbeddedKeys and let SelfUpdater call this from
// NewUpdater.
func BuildKeyResolver(cfg KeyResolverConfig, embeddedKeys ...[]byte) (KeyResolver, error) {
	source := strings.ToLower(strings.TrimSpace(cfg.KeySource))
	if source == "" {
		source = "both"
	}

	hasKeys := len(embeddedKeys) > 0
	hasEmail := strings.TrimSpace(cfg.ExternalKeyEmail) != ""

	switch source {
	case "embedded":
		if !hasKeys {
			return nil, errors.New("key_source=embedded requires embedded keys (WithEmbeddedKeys)")
		}

		return newEmbeddedResolverChecked(embeddedKeys...)
	case "external":
		if !hasEmail {
			return nil, errors.New("key_source=external requires update.external_key_email")
		}

		return newWKDFromConfig(cfg)
	case "both":
		return buildCompositeResolver(cfg, embeddedKeys, hasKeys, hasEmail)
	default:
		return nil, errors.Newf("unknown key_source %q (want embedded, external, or both)", source)
	}
}

// buildCompositeResolver handles the "both" key source, degrading to a
// single resolver when only one source is configured.
func buildCompositeResolver(cfg KeyResolverConfig, embeddedKeys [][]byte, hasKeys, hasEmail bool) (KeyResolver, error) {
	switch {
	case hasKeys && hasEmail:
		embedded, err := newEmbeddedResolverChecked(embeddedKeys...)
		if err != nil {
			return nil, err
		}

		wkd, err := newWKDFromConfig(cfg)
		if err != nil {
			return nil, err
		}

		return &CompositeResolver{
			Resolvers:  []KeyResolver{embedded, wkd},
			RequireAll: cfg.RequireExternalCrosscheck,
			Logger:     cfg.Logger,
		}, nil
	case hasKeys:
		return newEmbeddedResolverChecked(embeddedKeys...)
	case hasEmail:
		return newWKDFromConfig(cfg)
	default:
		return nil, errors.New("key_source=both requires embedded keys, an external key email, or both")
	}
}

func newWKDFromConfig(cfg KeyResolverConfig) (KeyResolver, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = gtbhttp.NewClient()
	}

	return NewWKDResolver(WKDResolverConfig{
		Email:      cfg.ExternalKeyEmail,
		HTTPClient: client,
	})
}
