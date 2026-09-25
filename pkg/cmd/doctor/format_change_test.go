package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0204 D14: doctor reports a config file stranded in the tool's previous
// format as a failure, with the refusal's hint, rather than as a first run.
func TestCheckConfig_StrandedInThePreviousFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".mytool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("log:\n  level: debug\n"), 0o600))

	props := &p.Props{
		Config: testutil.StoreFromYAML(t, "{}\n"),
		FS:     afero.NewOsFs(),
		Tool: p.Tool{
			Name:     "mytool",
			Config:   p.ConfigSpec{Format: "toml"},
			Features: p.SetFeatures(p.Enable(p.ConfigCmd)),
		},
	}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, CheckFail, result.Status)
	assert.Contains(t, result.Message, filepath.Join(dir, "config.yaml"))
	assert.Contains(t, result.Message, filepath.Join(dir, "config.toml"))
	assert.Contains(t, result.Details, "mytool config convert")
}
