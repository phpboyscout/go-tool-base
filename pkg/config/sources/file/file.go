// Package file links the file config source kind: a fixed file a tool reads
// beside its own, such as a platform-provided /etc/mytool/.env, in any format
// the tool links (spec 0204 D2, D3). A blank import registers it; `<tool>
// init config <name>` asks for the path. An absent file contributes nothing,
// as the tool's own files do.
package file

import (
	"context"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Kind is the kind's name in the manifest's config.sources.
const Kind = "file"

// ErrNoPath is a file slot with no path configured.
var ErrNoPath = errors.NewSentinel("gtb.config.sources.file.no_path", "file config source has no path")

func init() {
	setup.RegisterConfigSourceKind(Kind, factory, initialiser)
}

func factory(_ context.Context, settings config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
	path := settings.GetString("path")
	if path == "" {
		return nil, errors.WithHint(ErrNoPath, "set config.sources.<name>.path")
	}

	codec, err := b.CodecFor(path)
	if err != nil {
		return nil, err
	}

	return config.NewCodecBackend(b.FS(), path, codec), nil
}

func initialiser(_ *props.Props, slot props.ConfigSource) setup.Initialiser {
	return setup.SettingsInitialiser(slot, setup.SourceSetting{
		Key:         "path",
		Title:       "File",
		Description: "The config file this source reads; its extension says its format",
		Required:    true,
	})
}
