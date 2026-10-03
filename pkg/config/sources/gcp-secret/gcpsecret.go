// Package gcpsecret links the gcp-secret config source kind: one Secret
// Manager secret whose payload is a document, or a project's secrets, read
// as configuration (spec 0204 D3, D18). The client is built from gcpclient's
// ambient options and the store closes it; the adapter's FromOptions has no
// single-secret shape. Reading is sensitive and read-only.
package gcpsecret

import (
	"context"
	"io"

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
type opener func(ctx context.Context, settings config.Reader) (configgcpsecret.API, io.Closer, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openSecrets), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		setup.ReportSourceCredential(b, gcpsource.ChainName)

		if _, err := gcpsource.Project(settings); err != nil {
			return nil, err
		}

		secret := settings.GetString("secret")

		codec, err := valueCodec(settings, b, secret != "")
		if err != nil {
			return nil, err
		}

		opts, err := options(settings)
		if err != nil {
			return nil, err
		}

		api, closer, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		if closer != nil {
			setup.CloseWithStore(b, closer)
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

// valueCodec is the codec a secret's value decodes through: JSON for one
// secret unless the slot says otherwise, none for a project's secrets unless
// it asks for one.
func valueCodec(settings config.Reader, b setup.ConfigBootstrap, single bool) (config.Codec, error) {
	format := settings.GetString("value_format")
	if single && format == "" {
		format = defaultValueFormat
	}

	if format == "" {
		return nil, nil
	}

	return b.CodecFor("value." + format)
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

func openSecrets(ctx context.Context, settings config.Reader) (configgcpsecret.API, io.Closer, error) {
	clientOpts, err := gcpsource.ClientOptions(ctx, settings, gcpsource.ScopeCloudPlatform)
	if err != nil {
		return nil, nil, err
	}

	client, err := secretmanager.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, nil, errors.Wrap(err, "building the Secret Manager client")
	}

	return configgcpsecret.Wrap(client, settings.GetString("project"), settings.GetString("location")), client, nil
}
