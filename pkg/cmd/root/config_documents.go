package root

import (
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// warnExtraDocuments wraps a config file's codec so a file holding more than
// one YAML document says so. go/config still layers them, later over earlier
// (#181): the warning exists because a reader sees the first value and the
// tool uses the last.
func warnExtraDocuments(codec config.Codec, log logger.Logger) config.Codec {
	warn := documentCounter{base: codec, log: log}

	if editing, ok := codec.(config.EditingCodec); ok {
		return editingDocumentCounter{EditingCodec: editing, counter: warn}
	}

	return warn
}

type documentCounter struct {
	base config.Codec
	log  logger.Logger
}

// Decode implements config.Codec.
func (c documentCounter) Decode(path string, src []byte) ([]map[string]any, error) {
	docs, err := c.base.Decode(path, src)
	if err == nil && len(docs) > 1 {
		c.log.Warn("config file holds more than one document; each one overrides the one before it",
			"file", path, "documents", len(docs),
			"hint", "remove the extra --- separators so the file holds the values it shows first")
	}

	return docs, err
}

type editingDocumentCounter struct {
	config.EditingCodec
	counter documentCounter
}

// Decode implements config.Codec.
func (c editingDocumentCounter) Decode(path string, src []byte) ([]map[string]any, error) {
	return c.counter.Decode(path, src)
}
