package setup

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

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

// ErrAmbiguousProjectConfig is two project-local config files in one
// directory (spec 0204 D16).
var ErrAmbiguousProjectConfig = errors.NewSentinel("gtb.setup.ambiguous_project_config", "more than one project-local config file")

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

// IsProjectConfigName reports whether path is named like a project-local
// config file, ".<tool>" plus a YAML or family format extension, whether or
// not the format is linked.
func IsProjectConfigName(path, toolName string) bool {
	if toolName == "" {
		return false
	}

	base := filepath.Base(path)
	prefix := "." + toolName

	if !strings.HasPrefix(base, prefix) {
		return false
	}

	ext := strings.TrimPrefix(base, prefix)
	_, known := formatExtensions[ext]

	return known || ext == ".yaml" || ext == ".yml"
}

// ErrReadOnlyConfigFormat is a document that cannot be written because its
// format's codec cannot edit.
var ErrReadOnlyConfigFormat = errors.NewSentinel("gtb.setup.read_only_config_format", "config format is read-only")

// EncodeConfig writes doc as a new document through codec: YAML as the core
// marshals it, any other writable format by setting each leaf on an empty
// document.
func EncodeConfig(codec config.Codec, path string, doc map[string]any) ([]byte, error) {
	if _, ok := codec.(config.YAMLCodec); ok {
		out, err := yaml.Marshal(doc)

		return out, errors.Wrap(err, "encoding config")
	}

	editing, ok := codec.(config.EditingCodec)
	if !ok {
		return nil, errors.Wrapf(ErrReadOnlyConfigFormat, "%s", path)
	}

	var edits []config.Edit

	for _, leaf := range configLeaves(doc, "") {
		edits = append(edits, config.Edit{Path: leaf.path, Value: leaf.value})
	}

	out, err := editing.Apply(path, editing.Empty(), edits)

	return out, errors.Wrapf(err, "encoding %s", path)
}

// DecodeConfig reads src through codec into one document, later documents
// overriding earlier ones.
func DecodeConfig(codec config.Codec, path string, src []byte) (map[string]any, error) {
	docs, err := codec.Decode(path, src)
	if err != nil {
		return nil, err
	}

	merged := map[string]any{}
	for _, doc := range docs {
		maps.Copy(merged, doc)
	}

	return merged, nil
}

type configLeaf struct {
	path  string
	value any
}

// configLeaves lists every non-map value in doc by dotted path, in a stable
// order so an encoded document is reproducible.
func configLeaves(doc map[string]any, prefix string) []configLeaf {
	keys := slices.Sorted(maps.Keys(doc))

	var out []configLeaf

	for _, k := range keys {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}

		if child, ok := doc[k].(map[string]any); ok {
			out = append(out, configLeaves(child, path)...)

			continue
		}

		out = append(out, configLeaf{path: path, value: doc[k]})
	}

	return out
}
