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

// documentWarning wraps codec with the multi-document warning, except on a
// bootstrap pass, so each file warns once per load.
func (opts ConfigLoadOptions) documentWarning(codec config.Codec) config.Codec {
	if opts.bootstrap {
		return codec
	}

	return warnExtraDocuments(codec, opts.Props.Logger)
}

type documentCounter struct {
	base config.Codec
	log  logger.Logger
}

// Decode implements config.Codec.
func (c documentCounter) Decode(path string, src []byte) ([]map[string]any, error) {
	docs, err := c.base.Decode(path, src)
	if n := nonEmpty(docs); err == nil && n > 1 {
		c.log.Warn("config file holds more than one document; each one overrides the one before it",
			"file", path, "documents", n,
			"hint", "remove the extra --- separators so the file holds the values it shows first")
	}

	return docs, err
}

// PreservesComments implements config.CommentPreservingCodec, which the
// wrapper would otherwise hide.
func (c documentCounter) PreservesComments() bool {
	return preservesComments(c.base)
}

// preservesComments answers config.CommentPreservingCodec for the codec a
// wrapper decorates, so wrapping never changes go/config's capability report.
func preservesComments(codec config.Codec) bool {
	cp, ok := codec.(config.CommentPreservingCodec)

	return ok && cp.PreservesComments()
}

// nonEmpty counts the documents that set something: a trailing ---, or one
// holding only a comment, overrides nothing.
func nonEmpty(docs []map[string]any) int {
	n := 0

	for _, doc := range docs {
		if len(doc) > 0 {
			n++
		}
	}

	return n
}

type editingDocumentCounter struct {
	config.EditingCodec
	counter documentCounter
}

// Decode implements config.Codec.
func (c editingDocumentCounter) Decode(path string, src []byte) ([]map[string]any, error) {
	return c.counter.Decode(path, src)
}

// PreservesComments implements config.CommentPreservingCodec.
func (c editingDocumentCounter) PreservesComments() bool {
	return c.counter.PreservesComments()
}
