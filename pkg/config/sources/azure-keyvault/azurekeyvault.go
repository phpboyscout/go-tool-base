// Package azurekeyvault links the azure-keyvault config source kind: one
// Azure Key Vault secret whose value is a document, or every secret in the
// vault, read as configuration (spec 0204 D3, D18). The credential comes
// from azureclient's ambient chain. Reading is sensitive and read-only.
package azurekeyvault

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"gitlab.com/phpboyscout/go/config"
	configazurekeyvault "gitlab.com/phpboyscout/go/config-azure-keyvault"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/azuresource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourcesettings"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "azure-keyvault"

// defaultValueFormat is the usual shape of a secret holding a document.
const defaultValueFormat = "json"

// ErrNoVault is an azure-keyvault slot with no vault URL.
var ErrNoVault = errors.NewSentinel("gtb.config.sources.azure_keyvault.no_vault", "azure-keyvault config source has no vault_url")

// opener builds the vault client from a slot's settings: the Azure SDK in
// production, a fake in tests.
type opener func(ctx context.Context, settings config.Reader) (configazurekeyvault.SecretsAPI, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openVault), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		setup.ReportSourceCredential(b, azuresource.ChainName)

		if settings.GetString("vault_url") == "" {
			return nil, errors.WithHint(ErrNoVault, "set config.sources.<name>.vault_url, such as https://acme.vault.azure.net")
		}

		name := settings.GetString("name")

		format := settings.GetString("value_format")
		if name != "" && format == "" {
			format = defaultValueFormat
		}

		var codec config.Codec

		if format != "" {
			var err error
			if codec, err = b.CodecFor("value." + format); err != nil {
				return nil, err
			}
		}

		opts, err := options(settings)
		if err != nil {
			return nil, err
		}

		api, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		if name != "" {
			return configazurekeyvault.NewSecret(api, name, codec, opts...), nil
		}

		if codec != nil {
			opts = append(opts, configazurekeyvault.WithValueCodec(codec))
		}

		return configazurekeyvault.New(api, opts...), nil
	}
}

func options(settings config.Reader) ([]configazurekeyvault.Option, error) {
	var opts []configazurekeyvault.Option

	if prefix := settings.GetString("name_prefix"); prefix != "" {
		opts = append(opts, configazurekeyvault.WithNamePrefix(prefix))
	}

	interval, err := sourcesettings.PollInterval(settings)
	if err != nil {
		return nil, err
	}

	if interval > 0 {
		opts = append(opts, configazurekeyvault.WithPollInterval(interval))
	}

	return opts, nil
}

func openVault(ctx context.Context, settings config.Reader) (configazurekeyvault.SecretsAPI, error) {
	cred, err := azuresource.Credential(ctx, settings)
	if err != nil {
		return nil, err
	}

	client, err := azsecrets.NewClient(settings.GetString("vault_url"), cred, nil)
	if err != nil {
		return nil, errors.Wrap(err, "building the Key Vault client")
	}

	return configazurekeyvault.Wrap(client), nil
}
