package setup

import (
	"path/filepath"
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ConfigFormatPrefix prefixes a linked format's feature ID; see
// props.LinkedNames.
const ConfigFormatPrefix = "config-format-"

// SlotConfigCodec is where a format's link package contributes its codec.
const SlotConfigCodec features.Slot = "config-codec"

// ErrUnlinkedConfigFormat is a config file whose extension names a format the
// tool does not link (spec 0204 D2).
var ErrUnlinkedConfigFormat = errors.NewSentinel("gtb.setup.unlinked_config_format", "config file format not linked")

// formatExtensions are the extensions of the family's formats, linked or not.
// A path whose extension is none of these is read as YAML, as every path was
// before formats could be linked.
var formatExtensions = map[string]string{
	".toml":       "toml",
	".json":       "json",
	".hcl":        "hcl",
	".ini":        "ini",
	".xml":        "xml",
	".env":        "dotenv",
	".properties": "properties",
}

// ConfigCodec binds a linked format's codec to the file extensions it reads.
type ConfigCodec struct {
	Format     string
	Codec      config.Codec
	Extensions []string
}

// ConfigFormatDescriptor is the link feature a format's package declares. Like
// every link it defaults on, since its presence in the binary is its
// enablement (spec 0199 OQ3).
func ConfigFormatDescriptor(format string) props.FeatureDescriptor {
	return props.FeatureDescriptor{
		ID:      props.FeatureID(ConfigFormatPrefix + format),
		Kind:    props.KindLink,
		Default: true,
	}
}

// RegisterConfigCodec declares a format's link feature on the default
// registry and contributes its codec for the given extensions. A format's
// link package calls it from init.
func RegisterConfigCodec(format string, codec config.Codec, extensions ...string) {
	props.RegisterFeature(ConfigFormatDescriptor(format))
	RegisterConfigCodecOn(features.Default(), format, codec, extensions...)
}

// RegisterConfigCodecOn contributes a format's codec to a caller's registry,
// which must already declare ConfigFormatDescriptor(format).
func RegisterConfigCodecOn(r features.Registry, format string, codec config.Codec, extensions ...string) {
	r.Contribute(ConfigFormatDescriptor(format).ID, SlotConfigCodec, ConfigCodec{
		Format:     format,
		Codec:      codec,
		Extensions: extensions,
	})
}

// ConfigCodecsIn returns the codecs of every enabled feature in s.
func ConfigCodecsIn(s features.Set) []ConfigCodec {
	var out []ConfigCodec

	for _, d := range s.EnabledDescriptors() {
		codecs, _ := features.ContributionsOf[ConfigCodec](s, d.FeatureID(), SlotConfigCodec)
		out = append(out, codecs...)
	}

	return out
}

// ConfigCodecFor returns the codec that reads path, chosen by its extension.
// A linked format's extension gets its codec. Another format's extension is
// refused, naming what the tool accepts and the link that would read it. Any
// other path is YAML.
func ConfigCodecFor(codecs []ConfigCodec, path string) (config.Codec, error) {
	ext := strings.ToLower(filepath.Ext(path))

	for _, c := range codecs {
		if slices.Contains(c.Extensions, ext) {
			return c.Codec, nil
		}
	}

	format, known := formatExtensions[ext]
	if !known {
		return config.YAMLCodec{}, nil
	}

	return nil, errors.WithHintf(errors.Wrapf(ErrUnlinkedConfigFormat, "%s", path),
		"this tool reads %s; a %s file needs gitlab.com/phpboyscout/go-tool-base/pkg/config/formats/%s linked into it",
		strings.Join(acceptedExtensions(codecs), ", "), format, format)
}

func acceptedExtensions(codecs []ConfigCodec) []string {
	accepted := []string{".yaml", ".yml"}
	for _, c := range codecs {
		accepted = append(accepted, c.Extensions...)
	}

	return accepted
}
