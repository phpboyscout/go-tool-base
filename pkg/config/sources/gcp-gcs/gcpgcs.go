// Package gcpgcs links the gcp-gcs config source kind: one config file held
// as a Cloud Storage object, in the format its name says (spec 0204 D2, D3,
// D18). The client is built from gcpclient's ambient options and wrapped
// directly, which keeps the adapter's poll hint that its owned filesystem
// hides (go/config-gcp-gcs#2).
package gcpgcs

import (
	"context"

	"cloud.google.com/go/storage"

	"gitlab.com/phpboyscout/go/config"
	configgcpgcs "gitlab.com/phpboyscout/go/config-gcp-gcs"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/gcpsource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "gcp-gcs"

// ErrNoObject is a gcp-gcs slot without both a bucket and an object.
var ErrNoObject = errors.NewSentinel("gtb.config.sources.gcp_gcs.no_object", "gcp-gcs config source needs a bucket and an object")

// opener builds the bucket's filesystem from a slot's settings: the GCP SDK
// in production, a fake in tests.
type opener func(ctx context.Context, settings config.Reader) (config.FS, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openBucket), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		object := settings.GetString("object")
		if settings.GetString("bucket") == "" || object == "" {
			return nil, errors.WithHint(ErrNoObject, "set config.sources.<name>.bucket and .object")
		}

		codec, err := b.CodecFor(object)
		if err != nil {
			return nil, err
		}

		fsys, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		return config.NewCodecBackend(fsys, object, codec), nil
	}
}

func openBucket(ctx context.Context, settings config.Reader) (config.FS, error) {
	clientOpts, err := gcpsource.ClientOptions(ctx, settings, gcpsource.ScopeStorage)
	if err != nil {
		return nil, err
	}

	client, err := storage.NewClient(ctx, clientOpts...)
	if err != nil {
		return nil, errors.Wrap(err, "building the Cloud Storage client")
	}

	return configgcpgcs.Wrap(client, settings.GetString("bucket")), nil
}
