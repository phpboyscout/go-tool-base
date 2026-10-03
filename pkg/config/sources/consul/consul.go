// Package consul links the consul config source kind: the keys under a
// Consul KV prefix, read as configuration (spec 0204 D3, D18). The client
// starts from Consul's documented defaults, so CONSUL_HTTP_ADDR and
// CONSUL_HTTP_TOKEN apply unless the slot's settings say otherwise; a token
// may also come through config.sources.<name>.auth.*.
package consul

import (
	"context"

	capi "github.com/hashicorp/consul/api/v2"

	"gitlab.com/phpboyscout/go/config"
	configconsul "gitlab.com/phpboyscout/go/config-consul"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourceauth"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "consul"

// ErrNoPrefix is a consul slot with no KV prefix configured.
var ErrNoPrefix = errors.NewSentinel("gtb.config.sources.consul.no_prefix", "consul config source has no prefix")

func init() {
	setup.RegisterConfigSourceKind(Kind, factory, setup.ConfigSourceInitialiserFor(Kind))
}

func factory(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
	prefix := settings.GetString("prefix")
	if prefix == "" {
		return nil, errors.WithHint(ErrNoPrefix, "set config.sources.<name>.prefix")
	}

	cfg := capi.DefaultConfig()

	if address := settings.GetString("address"); address != "" {
		cfg.Address = address
	}

	if dc := settings.GetString("datacenter"); dc != "" {
		cfg.Datacenter = dc
	}

	token, err := sourceauth.Token(ctx, settings, b, Kind, "CONSUL_HTTP_TOKEN")
	if err != nil {
		return nil, err
	}

	if token != "" {
		cfg.Token = token
	}

	var opts []configconsul.Option

	// A value format decodes a blob stored under a key into a subtree, and
	// must be one the tool links.
	if format := settings.GetString("value_format"); format != "" {
		codec, err := b.CodecFor("value." + format)
		if err != nil {
			return nil, err
		}

		opts = append(opts, configconsul.WithValueCodec(codec))
	}

	return configconsul.FromConfig(cfg, prefix, opts...)
}
