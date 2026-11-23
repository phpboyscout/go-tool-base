package root

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

const twoDocuments = "log:\n  level: warn\n---\nlog:\n  level: debug\n"

// A config file with more than one YAML document keeps go/config's layering,
// later documents over earlier ones, and says so (#181).
func TestConfigDocuments_AUserFileWithTwoDocumentsWarns(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, p.ConfigDirXDG, fs, twoDocuments, false)

	log := logger.NewBuffer()
	view := buildUserStore(t, p.ConfigDirXDG, fs, log, userFile, "").View()

	assert.Equal(t, "debug", view.GetString("log.level"), "the later document still wins")
	assert.Equal(t, []string{userFile}, extraDocumentWarnings(log), "the extra document is reported, naming the file")
}

func TestConfigDocuments_AProjectFileWithTwoDocumentsWarns(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, p.ConfigDirXDG, fs, "log:\n  level: info\n", false)

	project := filepath.Join(t.TempDir(), ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, project, []byte(twoDocuments), 0o600))

	log := logger.NewBuffer()
	buildUserStore(t, p.ConfigDirXDG, fs, log, userFile, project)

	assert.Equal(t, []string{project}, extraDocumentWarnings(log))
}

func TestConfigDocuments_OneDocumentIsQuiet(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, p.ConfigDirXDG, fs, "log:\n  level: warn\n", false)

	log := logger.NewBuffer()
	buildUserStore(t, p.ConfigDirXDG, fs, log, userFile, "")

	assert.Empty(t, extraDocumentWarnings(log))
}

// A trailing separator, or a document holding only a comment, overrides
// nothing, so it is no reason to warn.
func TestConfigDocuments_AnEmptyExtraDocumentIsQuiet(t *testing.T) {
	for name, body := range map[string]string{
		"trailing separator": "log:\n  level: warn\n---\n",
		"comment only":       "log:\n  level: warn\n---\n# nothing here\n",
	} {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewOsFs()
			_, userFile := checkoutHome(t, p.ConfigDirXDG, fs, body, false)

			log := logger.NewBuffer()
			buildUserStore(t, p.ConfigDirXDG, fs, log, userFile, "")

			assert.Empty(t, extraDocumentWarnings(log))
		})
	}
}

func TestConfigDocuments_TheWarningKeepsCommentPreservation(t *testing.T) {
	cp, ok := warnExtraDocuments(config.YAMLCodec{}, logger.NewNoop()).(config.CommentPreservingCodec)

	require.True(t, ok, "the wrapper must still answer whether edits keep comments")
	assert.True(t, cp.PreservesComments())
}

// The bootstrap pass decodes the same files as the full store, so only the
// full store warns: once per file per load.
func TestConfigDocuments_TheBootstrapPassDoesNotWarn(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, p.ConfigDirXDG, fs, twoDocuments, false)

	log := logger.NewBuffer()
	props := &p.Props{Tool: p.Tool{Name: "mytool"}, Logger: log, FS: fs}

	_, err := bootstrapStore(t.Context(), ConfigLoadOptions{Props: props, CfgPaths: []string{userFile}}, props.Tool.ResolvedConfigSpec().Layers)
	require.NoError(t, err)

	assert.Empty(t, extraDocumentWarnings(log))
}

// A file config source decodes through the codec the bootstrap hands it, and
// it joins the full store, so it warns like any other config file.
func TestConfigDocuments_AConfigSourceFileWarns(t *testing.T) {
	log := logger.NewBuffer()
	props := &p.Props{Tool: p.Tool{Name: "mytool"}, Logger: log}

	b := sourceBootstrap{codecs: setup.ConfigCodecsIn(props.GetFeatures()), opts: ConfigLoadOptions{Props: props}}

	codec, err := b.CodecFor("/etc/mytool/extra.yaml")
	require.NoError(t, err)

	_, err = codec.Decode("/etc/mytool/extra.yaml", []byte(twoDocuments))
	require.NoError(t, err)

	assert.Equal(t, []string{"/etc/mytool/extra.yaml"}, extraDocumentWarnings(log))
}

func TestConfigDocuments_AWarnedFileStaysWritable(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, p.ConfigDirXDG, fs, twoDocuments, false)

	store := buildUserStore(t, p.ConfigDirXDG, fs, logger.NewNoop(), userFile, "")

	_, err := store.Apply(t.Context(), config.Set("telemetry.enabled", true))
	require.NoError(t, err)
}

// extraDocumentWarnings returns the file named by each warning about extra
// documents, in order.
func extraDocumentWarnings(log interface{ Entries() []logger.Entry }) []string {
	var files []string

	for _, e := range log.Entries() {
		if e.Level != logger.WarnLevel || !strings.Contains(e.Message, "more than one document") {
			continue
		}

		for i := 0; i+1 < len(e.Keyvals); i += 2 {
			if e.Keyvals[i] == "file" {
				files = append(files, fmt.Sprint(e.Keyvals[i+1]))
			}
		}
	}

	return files
}
