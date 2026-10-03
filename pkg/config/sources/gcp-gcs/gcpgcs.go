// Package gcpgcs links the gcp-gcs config source kind: one config file held
// as a Cloud Storage object, in the format its name says (spec 0204 D2, D3,
// D18). The filesystem is built through the adapter's FromOptions from
// gcpclient's ambient options, and the store closes the client it owns.
package gcpgcs

import (
	"context"
	"io"

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
type opener func(ctx context.Context, settings config.Reader) (config.FS, io.Closer, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openBucket), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		setup.ReportSourceCredential(b, gcpsource.ChainName)

		object := settings.GetString("object")
		if settings.GetString("bucket") == "" || object == "" {
			return nil, errors.WithHint(ErrNoObject, "set config.sources.<name>.bucket and .object")
		}

		codec, err := b.CodecFor(object)
		if err != nil {
			return nil, err
		}

		fsys, closer, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		if closer != nil {
			setup.CloseWithStore(b, closer)
		}

		return config.NewCodecBackend(fsys, object, codec), nil
	}
}

func openBucket(ctx context.Context, settings config.Reader) (config.FS, io.Closer, error) {
	clientOpts, err := gcpsource.ClientOptions(ctx, settings, gcpsource.ScopeStorage)
	if err != nil {
		return nil, nil, err
	}

	fsys, err := configgcpgcs.FromOptions(ctx, settings.GetString("bucket"), clientOpts)
	if err != nil {
		return nil, nil, errors.Wrap(err, "building the Cloud Storage client")
	}

	return fsys, fsys, nil
}
