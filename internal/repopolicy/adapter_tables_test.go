package repopolicy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestGenerateReferenceAdapterTablesMatchTheCode (#73): the adapter table in
// docs/reference/cli/generate.md is read against chat.ProviderModules and
// forge.ModuleFor, so a provider or forge added to the code without its row,
// or a row naming the wrong module, fails here rather than drifting.
func TestGenerateReferenceAdapterTablesMatchTheCode(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join(repoRoot(t), "docs", "reference", "cli", "generate.md"))
	require.NoError(t, err)

	chatRow := tableRow(t, string(src), "`cmd/<name>/chat.go`")
	forgeRow := tableRow(t, string(src), "`cmd/<name>/forge.go`")

	for _, entry := range chat.ProviderModules() {
		want := "`" + string(entry.Provider) + "`"
		module := "`go/" + strings.TrimPrefix(entry.Module, "gitlab.com/phpboyscout/go/") + "`"

		assert.Containsf(t, chatRow, want, "chat.go row omits provider %s", entry.Provider)
		assert.Containsf(t, chatRow, module, "chat.go row omits module %s", entry.Module)
		assert.Truef(t, mapsTo(chatRow, want, module), "chat.go row does not map %s to %s", entry.Provider, entry.Module)
	}

	for _, d := range forge.Displays() {
		module, ok := forge.ModuleFor(string(d.ID))
		require.Truef(t, ok, "no module for forge %s", d.ID)

		want := "`" + string(d.ID) + "`"
		short := "`go/" + strings.TrimPrefix(module, "gitlab.com/phpboyscout/go/") + "`"

		assert.Containsf(t, forgeRow, want, "forge.go row omits forge %s", d.ID)
		assert.Truef(t, mapsTo(forgeRow, want, short), "forge.go row does not map %s to %s", d.ID, module)
	}
}

// tableRow returns the markdown table row whose first cell is name.
func tableRow(t *testing.T, src, name string) string {
	t.Helper()

	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(line, "| "+name+" |") {
			return line
		}
	}

	t.Fatalf("no table row for %s", name)

	return ""
}

// mapsTo reports whether the row's module cell carries a "names → module"
// group in which name appears before module and no other module intervenes.
func mapsTo(row, name, module string) bool {
	cells := strings.Split(row, "|")
	groups := regexp.MustCompile(`;\s*`).Split(cells[len(cells)-2], -1)

	for _, g := range groups {
		if strings.Contains(g, name) && strings.HasSuffix(strings.TrimSpace(g), "→ "+module) {
			return true
		}
	}

	return false
}
