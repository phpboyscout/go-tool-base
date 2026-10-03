// Package azureblob links the azure-blob config source kind: one config file
// held as an Azure Storage blob, in the format its name says (spec 0204 D2,
// D3, D18). The credential comes from azureclient's ambient chain.
package azureblob

import (
	"context"

	"gitlab.com/phpboyscout/go/config"
	configazureblob "gitlab.com/phpboyscout/go/config-azure-blob"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/azuresource"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "azure-blob"

// ErrNoBlob is an azure-blob slot without a service URL, container and blob.
var ErrNoBlob = errors.NewSentinel("gtb.config.sources.azure_blob.no_blob", "azure-blob config source needs a service_url, container and blob")

// opener builds the container's filesystem from a slot's settings: the Azure
// SDK in production, a fake in tests.
type opener func(ctx context.Context, settings config.Reader) (config.FS, error)

func init() {
	setup.RegisterConfigSourceKind(Kind, factoryWith(openContainer), setup.ConfigSourceInitialiserFor(Kind))
}

func factoryWith(open opener) setup.SourceFactory {
	return func(ctx context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		setup.ReportSourceCredential(b, azuresource.ChainName)

		blob := settings.GetString("blob")
		if settings.GetString("service_url") == "" || settings.GetString("container") == "" || blob == "" {
			return nil, errors.WithHint(ErrNoBlob, "set config.sources.<name>.service_url, .container and .blob")
		}

		codec, err := b.CodecFor(blob)
		if err != nil {
			return nil, err
		}

		fsys, err := open(ctx, settings)
		if err != nil {
			return nil, err
		}

		return config.NewCodecBackend(fsys, blob, codec), nil
	}
}

func openContainer(ctx context.Context, settings config.Reader) (config.FS, error) {
	cred, err := azuresource.Credential(ctx, settings)
	if err != nil {
		return nil, err
	}

	return configazureblob.FSFromCredential(cred, settings.GetString("service_url"), settings.GetString("container"))
}
