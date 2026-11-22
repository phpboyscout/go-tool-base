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
)

const twoDocuments = "log:\n  level: warn\n---\nlog:\n  level: debug\n"

// A config file with more than one YAML document keeps go/config's layering,
// later documents over earlier ones, and says so (#181).
func TestConfigDocuments_AUserFileWithTwoDocumentsWarns(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, twoDocuments, false)

	log := logger.NewBuffer()
	view := buildUserStore(t, fs, log, userFile, "").View()

	assert.Equal(t, "debug", view.GetString("log.level"), "the later document still wins")
	assert.Equal(t, []string{userFile}, extraDocumentWarnings(log), "the extra document is reported, naming the file")
}

func TestConfigDocuments_AProjectFileWithTwoDocumentsWarns(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, "log:\n  level: info\n", false)

	project := filepath.Join(t.TempDir(), ".mytool.yaml")
	require.NoError(t, afero.WriteFile(fs, project, []byte(twoDocuments), 0o600))

	log := logger.NewBuffer()
	buildUserStore(t, fs, log, userFile, project)

	assert.Equal(t, []string{project}, extraDocumentWarnings(log))
}

func TestConfigDocuments_OneDocumentIsQuiet(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, "log:\n  level: warn\n", false)

	log := logger.NewBuffer()
	buildUserStore(t, fs, log, userFile, "")

	assert.Empty(t, extraDocumentWarnings(log))
}

func TestConfigDocuments_AWarnedFileStaysWritable(t *testing.T) {
	fs := afero.NewOsFs()
	_, userFile := checkoutHome(t, fs, twoDocuments, false)

	store := buildUserStore(t, fs, logger.NewNoop(), userFile, "")

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
