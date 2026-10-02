// Package awsssm links the aws-ssm config source kind: every AWS Systems
// Manager parameter under a path prefix, read as configuration (spec 0204
// D3, D18). The AWS config comes from awsclient's ambient chain, with the
// slot's region, profile and endpoint applied.
package awsssm

import (
	"context"

	"gitlab.com/phpboyscout/go/config"
	configawsssm "gitlab.com/phpboyscout/go/config-aws-ssm"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/sourcesettings"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "aws-ssm"

// ErrNoPrefix is an aws-ssm slot with no parameter path prefix.
var ErrNoPrefix = errors.NewSentinel("gtb.config.sources.aws_ssm.no_prefix", "aws-ssm config source has no prefix")

func init() {
	setup.RegisterConfigSourceKind(Kind, factory, initialiser)
}

func factory(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
	prefix := settings.GetString("prefix")
	if prefix == "" {
		return nil, errors.WithHint(ErrNoPrefix, "set config.sources.<name>.prefix, a parameter path such as /team/mytool")
	}

	cfg, err := awssource.Config(ctx, settings)
	if err != nil {
		return nil, err
	}

	var opts []configawsssm.Option

	if format := settings.GetString("value_format"); format != "" {
		codec, err := b.CodecFor("value." + format)
		if err != nil {
			return nil, err
		}

		opts = append(opts, configawsssm.WithValueCodec(codec))
	}

	interval, err := sourcesettings.PollInterval(settings)
	if err != nil {
		return nil, err
	}

	if interval > 0 {
		opts = append(opts, configawsssm.WithPollInterval(interval))
	}

	return configawsssm.FromConfig(cfg, prefix, opts...)
}

func initialiser(_ *props.Props, slot props.ConfigSource) setup.Initialiser {
	return setup.SettingsInitialiser(slot, append([]setup.SourceSetting{
		{Key: "prefix", Title: "Parameter path prefix", Description: "Every parameter under it is read, such as /team/mytool", Required: true},
	}, awssource.Settings()...)...)
}
