// Package awss3 links the aws-s3 config source kind: one config file held as
// an S3 object, in the format its key names (spec 0204 D2, D3, D18). The AWS
// config comes from awsclient's ambient chain, with the slot's region,
// profile and endpoint applied, and path-style addressing is a setting for
// MinIO and LocalStack.
package awss3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"gitlab.com/phpboyscout/go/config"
	configawss3 "gitlab.com/phpboyscout/go/config-aws-s3"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "aws-s3"

// ErrNoObject is an aws-s3 slot without both a bucket and a key.
var ErrNoObject = errors.NewSentinel("gtb.config.sources.aws_s3.no_object", "aws-s3 config source needs a bucket and a key")

func init() {
	setup.RegisterConfigSourceKind(Kind, factory, setup.ConfigSourceInitialiserFor(Kind))
}

// factory builds the S3 client itself rather than through the adapter's
// FSFromConfig, which offers no path-style addressing.
func factory(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
	bucket, key := settings.GetString("bucket"), settings.GetString("key")
	if bucket == "" || key == "" {
		return nil, errors.WithHint(ErrNoObject, "set config.sources.<name>.bucket and .key")
	}

	codec, err := b.CodecFor(key)
	if err != nil {
		return nil, err
	}

	cfg, err := awssource.Config(ctx, settings)
	if err != nil {
		return nil, err
	}

	pathStyle := settings.GetBool("path_style")
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = pathStyle })

	var opts []configawss3.Option
	if prefix := settings.GetString("key_prefix"); prefix != "" {
		opts = append(opts, configawss3.WithKeyPrefix(prefix))
	}

	return config.NewCodecBackend(configawss3.Wrap(client, bucket, opts...), key, codec), nil
}
