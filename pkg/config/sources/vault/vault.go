// Package vault links the vault config source kind: a HashiCorp Vault KV v2
// secret, or every secret under a prefix, read as configuration (spec 0204
// D3, D18). The client comes from vaultclient's ambient chain, so VAULT_ADDR,
// VAULT_TOKEN and VAULT_NAMESPACE apply unless the slot's settings say
// otherwise; a token may also come through config.sources.<name>.auth.*.
// Reading is sensitive and read-only.
package vault

import (
	"context"
	"time"

	"gitlab.com/phpboyscout/go/config"
	configvault "gitlab.com/phpboyscout/go/config-vault"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/vaultclient"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourceauth"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "vault"

// defaultMount is where Vault mounts KV v2 out of the box.
const defaultMount = "secret"

// ErrNoPath is a vault slot naming neither a secret path nor a prefix, or
// both.
var ErrNoPath = errors.NewSentinel("gtb.config.sources.vault.no_path", "vault config source needs one of path or prefix")

func init() {
	setup.RegisterConfigSourceKind(Kind, factory, setup.ConfigSourceInitialiserFor(Kind))
}

func factory(ctx context.Context, settings config.Reader, _ setup.ConfigBootstrap) (config.Backend, error) {
	path, prefix := settings.GetString("path"), settings.GetString("prefix")
	if (path == "") == (prefix == "") {
		return nil, errors.WithHint(ErrNoPath, "set config.sources.<name>.path for one secret, or .prefix for every secret beneath it")
	}

	client, err := vaultclient.Ambient(
		vaultclient.WithAddress(settings.GetString("address")),
		vaultclient.WithNamespace(settings.GetString("namespace")),
	).VaultClient(ctx)
	if err != nil {
		return nil, err
	}

	token, err := sourceauth.Token(ctx, settings, Kind)
	if err != nil {
		return nil, err
	}

	if token != "" {
		client.SetToken(token)
	}

	opts, err := options(settings)
	if err != nil {
		return nil, err
	}

	mount := settings.GetString("mount")
	if mount == "" {
		mount = defaultMount
	}

	if prefix != "" {
		return configvault.FromClientPrefix(client, mount, prefix, opts...), nil
	}

	return configvault.FromClient(client, mount, path, opts...), nil
}

func options(settings config.Reader) ([]configvault.Option, error) {
	interval := settings.GetString("poll_interval")
	if interval == "" {
		return nil, nil
	}

	d, err := time.ParseDuration(interval)
	if err != nil {
		return nil, errors.Wrap(err, "poll_interval")
	}

	return []configvault.Option{configvault.WithPollInterval(d)}, nil
}
