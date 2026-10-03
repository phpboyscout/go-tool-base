// Package azureappconfig links the azure-appconfig config source kind: the
// settings under a key prefix in an Azure App Configuration store, read as
// configuration (spec 0204 D3, D18). The credential comes from azureclient's
// ambient chain.
package azureappconfig

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/data/azappconfig/v2"

	"gitlab.com/phpboyscout/go/config"
	configazureappconfig "gitlab.com/phpboyscout/go/config-azure-appconfig"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/azuresource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "azure-appconfig"

// ErrNoEndpoint is an azure-appconfig slot with no store endpoint.
var ErrNoEndpoint = errors.NewSentinel("gtb.config.sources.azure_appconfig.no_endpoint", "azure-appconfig config source has no endpoint")

// opener builds the store client from a slot's settings: the Azure SDK in
// production, a fake in tests.
type opener func(ctx context.Context, settings config.Reader) (configazureappconfig.Store, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openStore), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		if settings.GetString("endpoint") == "" {
			return nil, errors.WithHint(ErrNoEndpoint, "set config.sources.<name>.endpoint, such as https://acme.azconfig.io")
		}

		var opts []configazureappconfig.Option

		if label := settings.GetString("label"); label != "" {
			opts = append(opts, configazureappconfig.WithLabel(label))
		}

		if sentinel := settings.GetString("sentinel_key"); sentinel != "" {
			opts = append(opts, configazureappconfig.WithSentinelKey(sentinel))
		}

		if format := settings.GetString("value_format"); format != "" {
			codec, err := b.CodecFor("value." + format)
			if err != nil {
				return nil, err
			}

			opts = append(opts, configazureappconfig.WithValueCodec(codec))
		}

		store, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		return configazureappconfig.New(store, settings.GetString("prefix"), opts...), nil
	}
}

func openStore(ctx context.Context, settings config.Reader) (configazureappconfig.Store, error) {
	cred, err := azuresource.Credential(ctx, settings)
	if err != nil {
		return nil, err
	}

	client, err := azappconfig.NewClient(settings.GetString("endpoint"), cred, nil)
	if err != nil {
		return nil, errors.Wrap(err, "building the App Configuration client")
	}

	return configazureappconfig.Wrap(client), nil
}
