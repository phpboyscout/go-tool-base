// Package awssecrets links the aws-secrets config source kind: one AWS
// Secrets Manager secret whose value is a document, or every secret under a
// name prefix, read as configuration (spec 0204 D3, D18). The AWS config
// comes from awsclient's ambient chain, with the slot's region, profile and
// endpoint applied. Reading is sensitive and read-only.
package awssecrets

import (
	"context"

	"gitlab.com/phpboyscout/go/config"
	configawssecrets "gitlab.com/phpboyscout/go/config-aws-secrets"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourcesettings"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "aws-secrets"

// defaultValueFormat is how AWS's own tooling writes a secret's value.
const defaultValueFormat = "json"

// ErrNoName is an aws-secrets slot naming neither a secret nor a prefix, or
// both.
var ErrNoName = errors.NewSentinel("gtb.config.sources.aws_secrets.no_name", "aws-secrets config source needs one of name or prefix")

func init() {
	setup.RegisterConfigSourceKind(Kind, factory, initialiser)
}

func factory(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
	name, prefix := settings.GetString("name"), settings.GetString("prefix")
	if (name == "") == (prefix == "") {
		return nil, errors.WithHint(ErrNoName, "set config.sources.<name>.name for one secret, or .prefix for every secret under it")
	}

	opts, err := options(settings)
	if err != nil {
		return nil, err
	}

	format := settings.GetString("value_format")

	// One secret's value is an opaque string until decoded, so its format is
	// not optional; under a prefix the names supply the structure.
	if name != "" && format == "" {
		format = defaultValueFormat
	}

	var codec config.Codec

	if format != "" {
		if codec, err = b.CodecFor("value." + format); err != nil {
			return nil, err
		}
	}

	cfg, err := awssource.Config(ctx, settings)
	if err != nil {
		return nil, err
	}

	if name != "" {
		return configawssecrets.FromConfigSecret(cfg, name, codec, opts...)
	}

	if codec != nil {
		opts = append(opts, configawssecrets.WithValueCodec(codec))
	}

	return configawssecrets.FromConfig(cfg, prefix, opts...)
}

func options(settings config.Reader) ([]configawssecrets.Option, error) {
	var opts []configawssecrets.Option

	if stage := settings.GetString("version_stage"); stage != "" {
		opts = append(opts, configawssecrets.WithVersionStage(stage))
	}

	interval, err := sourcesettings.PollInterval(settings)
	if err != nil {
		return nil, err
	}

	if interval > 0 {
		opts = append(opts, configawssecrets.WithPollInterval(interval))
	}

	return opts, nil
}

func initialiser(_ *props.Props, slot props.ConfigSource) setup.Initialiser {
	return setup.SettingsInitialiser(slot, append([]setup.SourceSetting{
		{Key: "name", Title: "Secret name", Description: "The secret whose JSON value is read as configuration", Required: true},
	}, awssource.Settings()...)...)
}
