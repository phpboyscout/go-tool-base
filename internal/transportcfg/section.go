// Package transportcfg holds config-resolution helpers shared by the pkg/grpc
// and pkg/http server adapters.
//
// Both adapters resolve the same resilience subsections — rate limiting and
// circuit breaking — from configuration, against the per-transport go/transit
// types. Those types are distinct (transit/grpc versus transit/http) but their
// config-resolution shape is identical, so the resolution lives here once and
// each adapter binds its own type.
package transportcfg

import "gitlab.com/phpboyscout/go/config"

// ResolveSection resolves the config subsection at base into T and merges it
// over the type's defaults, honouring which of the type's fields the caller
// actually set.
//
// It captures the pattern the transport adapters otherwise repeat per
// resilience section: return defaults() when cfg is nil, the section is absent,
// or the typed decode fails (a malformed value must not discard the shipped
// defaults); otherwise merge(defaults(), parsed, overrides). The per-field
// "was it set" flags come from overrides, which receives the reader's IsSet so
// each caller names only its own subkeys.
func ResolveSection[T, O any](
	cfg config.Reader,
	base string,
	defaults func() T,
	merge func(base, overlay T, overrides O) T,
	overrides func(isSet func(string) bool) O,
) T {
	if cfg == nil {
		return defaults()
	}

	section, err := config.UnmarshalSection[T](cfg, base)
	if err != nil || !section.Exists {
		return defaults()
	}

	return merge(defaults(), section.Value, overrides(cfg.IsSet))
}
