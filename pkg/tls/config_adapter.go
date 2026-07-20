package tls

import "gitlab.com/phpboyscout/go/config"

// Resolve resolves the TLS settings for a transport from GTB config. It starts
// from the shared SharedPrefix ("server.tls") and overrides each field
// individually from the transport-specific prefix (e.g. "server.grpc.tls",
// "server.http.tls", "server.gateway.tls") whenever that key is set.
func Resolve(cfg config.Reader, transportPrefix string) Pair {
	if cfg == nil {
		return Pair{}
	}

	shared := pairFromConfig(cfg, SharedPrefix)
	transport := pairFromConfig(cfg, transportPrefix)

	return ResolvePair(shared, transport, PairOverrides{
		Enabled: cfg.IsSet(transportPrefix + ".enabled"),
		Cert:    cfg.IsSet(transportPrefix + ".cert"),
		Key:     cfg.IsSet(transportPrefix + ".key"),
	})
}

func pairFromConfig(cfg config.Reader, prefix string) Pair {
	section, err := config.UnmarshalSection[Pair](cfg, prefix)
	if err != nil || !section.Exists {
		return Pair{}
	}

	return section.Value
}
