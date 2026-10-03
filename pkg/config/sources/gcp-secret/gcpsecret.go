// Package gcpsecret links the gcp-secret config source kind: one Secret
// Manager secret whose payload is a document, or a project's secrets, read
// as configuration (spec 0204 D3, D18). The client is built from gcpclient's
// ambient options and handed to the adapter's FromClient rather than built
// through FromOptions, whose owned backend hides the watch
// (go/config-gcp-secret#2). Reading is sensitive and read-only.
package gcpsecret

import (
	"context"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"

	"gitlab.com/phpboyscout/go/config"
	configgcpsecret "gitlab.com/phpboyscout/go/config-gcp-secret"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourcesettings"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "gcp-secret"

// defaultValueFormat is the usual shape of a secret holding a document.
const defaultValueFormat = "json"

// opener builds the Secret Manager client from a slot's settings: the GCP SDK
// in production, a fake in tests.
type opener func(ctx context.Context, settings config.Reader) (configgcpsecret.API, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openSecrets), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		if _, err := gcpsource.Project(settings); err != nil {
			return nil, err
		}

		secret := settings.GetString("secret")

		format := settings.GetString("value_format")
		if secret != "" && format == "" {
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

		if secret != "" {
			return configgcpsecret.NewSecret(api, secret, codec, opts...), nil
		}

		if codec != nil {
			opts = append(opts, configgcpsecret.WithValueCodec(codec))
		}

		return configgcpsecret.New(api, opts...), nil
	}
}

func options(settings config.Reader) ([]configgcpsecret.Option, error) {
	var opts []configgcpsecret.Option

	if prefix := settings.GetString("name_prefix"); prefix != "" {
		opts = append(opts, configgcpsecret.WithNamePrefix(prefix))
	}

	if version := settings.GetString("version"); version != "" {
		opts = append(opts, configgcpsecret.WithVersion(version))
	}

	interval, err := sourcesettings.PollInterval(settings)
	if err != nil {
		return nil, err
	}

	if interval > 0 {
		opts = append(opts, configgcpsecret.WithPollInterval(interval))
	}

	return opts, nil
}

func openSecrets(ctx context.Context, settings config.Reader) (configgcpsecret.API, error) {
	clientOpts, err := gcpsource.ClientOptions(ctx, settings, gcpsource.ScopeCloudPlatform)
	if err != nil {
		return nil, err
	}

	client, err := secretmanager.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "building the Secret Manager client")
	}

	return configgcpsecret.Wrap(client, settings.GetString("project"), settings.GetString("location")), nil
}
