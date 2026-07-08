package tls

// Pair is the typed enabled/cert/key triple used to configure TLS for any
// transport. It carries struct tags so the same shape marshals to and from
// config consistently wherever it is used.
type Pair struct {
	Enabled bool   `mapstructure:"enabled" yaml:"enabled" json:"enabled"`
	Cert    string `mapstructure:"cert"    yaml:"cert"    json:"cert"`
	Key     string `mapstructure:"key"     yaml:"key"     json:"key"`
}

// PairOverrides records which fields a transport-specific TLS section set.
// ResolvePair uses it to merge per-transport config without requiring a config
// lookup interface.
type PairOverrides struct {
	Enabled bool
	Cert    bool
	Key     bool
}

// ResolvePair resolves TLS settings from already-materialised typed values. It
// starts from the shared pair and overrides individual fields when the
// transport section explicitly supplied them.
func ResolvePair(shared Pair, transport Pair, overrides PairOverrides) Pair {
	pair := shared

	if overrides.Enabled {
		pair.Enabled = transport.Enabled
	}

	if overrides.Cert {
		pair.Cert = transport.Cert
	}

	if overrides.Key {
		pair.Key = transport.Key
	}

	return pair
}
