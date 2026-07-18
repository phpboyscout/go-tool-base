package tls

import "gitlab.com/phpboyscout/go/config"

// Resolve resolves the TLS settings for a transport from GTB config. It starts
// from the shared SharedPrefix ("server.tls") and overrides each field
// individually from the transport-specific prefix (e.g. "server.grpc.tls",
// "server.http.tls", "server.gateway.tls") whenever that key is set.
func Resolve(cfg config.Containable, transportPrefix string) Pair {
	if cfg == nil {
		return Pair{}
	}

	if _, ok := cfg.(*config.Container); !ok {
		return resolveLegacy(cfg, transportPrefix)
	}

	shared := pairFromConfig(cfg, SharedPrefix)
	transport := pairFromConfig(cfg, transportPrefix)

	return ResolvePair(shared, transport, PairOverrides{
		Enabled: cfg.IsSet(transportPrefix + ".enabled"),
		Cert:    cfg.IsSet(transportPrefix + ".cert"),
		Key:     cfg.IsSet(transportPrefix + ".key"),
	})
}

func pairFromConfig(cfg config.Containable, prefix string) Pair {
	section, err := config.UnmarshalSection[Pair](cfg, prefix)
	if err != nil || !section.Exists {
		return Pair{}
	}

	return section.Value
}

func resolveLegacy(cfg config.Containable, transportPrefix string) Pair {
	pair := Pair{
		Enabled: cfg.GetBool(SharedPrefix + ".enabled"),
		Cert:    cfg.GetString(SharedPrefix + ".cert"),
		Key:     cfg.GetString(SharedPrefix + ".key"),
	}

	if cfg.IsSet(transportPrefix + ".enabled") {
		pair.Enabled = cfg.GetBool(transportPrefix + ".enabled")
	}

	if cfg.IsSet(transportPrefix + ".cert") {
		pair.Cert = cfg.GetString(transportPrefix + ".cert")
	}

	if cfg.IsSet(transportPrefix + ".key") {
		pair.Key = cfg.GetString(transportPrefix + ".key")
	}

	return pair
}
