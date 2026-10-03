// Package gcpparameter links the gcp-parameter config source kind: one
// Parameter Manager parameter whose payload is a document, or every
// parameter under an ID prefix, read as configuration (spec 0204 D3, D18).
// The client is built from gcpclient's ambient options and handed to the
// adapter's FromClient, whose backend keeps its watch.
package gcpparameter

import (
	"context"

	parametermanager "cloud.google.com/go/parametermanager/apiv1"

	"gitlab.com/phpboyscout/go/config"
	configgcpparameter "gitlab.com/phpboyscout/go/config-gcp-parameter"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourcesettings"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "gcp-parameter"

// defaultLocation is Parameter Manager's global service.
const defaultLocation = "global"

// ErrNoParameter is a gcp-parameter slot naming neither a parameter nor a
// prefix, or both.
var ErrNoParameter = errors.NewSentinel("gtb.config.sources.gcp_parameter.no_parameter", "gcp-parameter config source needs one of parameter or prefix")

// opener builds the Parameter Manager client from a slot's settings: the GCP
// SDK in production, a fake in tests.
type opener func(ctx context.Context, settings config.Reader) (configgcpparameter.PM, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openParameters), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		setup.ReportSourceCredential(b, gcpsource.ChainName)

		if _, err := gcpsource.Project(settings); err != nil {
			return nil, err
		}

		parameter, prefix := settings.GetString("parameter"), settings.GetString("prefix")
		if (parameter == "") == (prefix == "") {
			return nil, errors.WithHint(ErrNoParameter, "set config.sources.<name>.parameter for one document, or .prefix for every parameter under it")
		}

		opts, err := options(settings, b, parameter != "")
		if err != nil {
			return nil, err
		}

		pm, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		if prefix != "" {
			return configgcpparameter.NewPrefix(pm, prefix, opts...), nil
		}

		return configgcpparameter.New(pm, parameter, opts...), nil
	}
}

// options reads the slot's value format and poll interval. One parameter's
// payload is a document, YAML unless the slot says otherwise, which needs no
// link.
func options(settings config.Reader, b setup.ConfigBootstrap, single bool) ([]configgcpparameter.Option, error) {
	format := settings.GetString("value_format")
	if single && format == "" {
		format = "yaml"
	}

	var opts []configgcpparameter.Option

	if format != "" {
		codec, err := b.CodecFor("value." + format)
		if err != nil {
			return nil, err
		}

		opts = append(opts, configgcpparameter.WithValueCodec(codec))
	}

	interval, err := sourcesettings.PollInterval(settings)
	if err != nil {
		return nil, err
	}

	if interval > 0 {
		opts = append(opts, configgcpparameter.WithPollInterval(interval))
	}

	return opts, nil
}

func openParameters(ctx context.Context, settings config.Reader) (configgcpparameter.PM, error) {
	clientOpts, err := gcpsource.ClientOptions(ctx, settings, gcpsource.ScopeCloudPlatform)
	if err != nil {
		return nil, err
	}

	client, err := parametermanager.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "building the Parameter Manager client")
	}

	location := settings.GetString("location")
	if location == "" {
		location = defaultLocation
	}

	return configgcpparameter.Wrap(client, settings.GetString("project"), location), nil
}
