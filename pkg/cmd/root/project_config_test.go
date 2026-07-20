package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestDiscoverProjectConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// a repo with the project config at its root, and a nested working dir
	_ = fs.MkdirAll("/repo/sub/deep", 0o755)
	_ = afero.WriteFile(fs, "/repo/.keryx.yaml", []byte("themes: {}\n"), 0o644)

	// walks up from a nested dir to the repo-root config
	if got := discoverProjectConfig(fs, "keryx", "/repo/sub/deep"); got != "/repo/.keryx.yaml" {
		t.Errorf("nested cwd: got %q, want /repo/.keryx.yaml", got)
	}

	// found at the dir itself
	if got := discoverProjectConfig(fs, "keryx", "/repo"); got != "/repo/.keryx.yaml" {
		t.Errorf("repo root: got %q", got)
	}

	// a different tool name → not matched
	if got := discoverProjectConfig(fs, "othertool", "/repo/sub"); got != "" {
		t.Errorf("wrong tool name should not match: %q", got)
	}

	// no config anywhere above → ""
	_ = fs.MkdirAll("/elsewhere", 0o755)
	if got := discoverProjectConfig(fs, "keryx", "/elsewhere"); got != "" {
		t.Errorf("absent: got %q, want \"\"", got)
	}

	// empty inputs
	if discoverProjectConfig(fs, "", "/repo") != "" || discoverProjectConfig(fs, "keryx", "") != "" {
		t.Error("empty tool/dir should return \"\"")
	}

}

// TestProjectConfigPaths_ExplicitConfigSuppressesTheProjectLayer pins the rule that
// an explicitly named --config is authoritative.
//
// The flag is declared as a StringArray whose default value IS the standard paths,
// so supplying it replaces them outright rather than adding to them — naming a config
// file means "use this one". Appending the project-local layer on top of that, as this
// once did, let a .<tool>.yaml the caller never named override the file they did name.
// The failure was silent: the tool ran against different settings with nothing on the
// command line to account for it.
//
// Both directions matter, so both are asserted here. Only checking the suppression
// would pass just as well if the layer had been removed altogether.
func TestProjectConfigPaths_ExplicitConfigSuppressesTheProjectLayer(t *testing.T) {
	// Not parallel: t.Chdir is process-wide, and projectConfigPaths reads the
	// working directory through os.Getwd.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".keryx.yaml"), []byte("themes: {}\n"), 0o600))
	t.Chdir(dir)

	// A real filesystem, because the discovery walk and os.Getwd must agree on
	// what exists — an in-memory one would not contain the directory t.Chdir
	// just moved into.
	props := &p.Props{
		Tool:   p.Tool{Name: "keryx"},
		Logger: logger.NewNoop(),
		FS:     afero.NewOsFs(),
	}

	named := []string{"/my/exact/file.yaml"}

	withFlag := newConfigFlagCmd(t, named...)
	assert.Equal(t, named, projectConfigPaths(props, withFlag, named),
		"an explicit --config must not be overridden by a project-local file")

	// Without the flag the layer still applies, appended last so it wins over
	// the defaults it was designed to override.
	defaults := []string{"/etc/keryx/config.yaml"}

	withoutFlag := newConfigFlagCmd(t)
	assert.Equal(t,
		append(append([]string{}, defaults...), filepath.Join(dir, ".keryx.yaml")),
		projectConfigPaths(props, withoutFlag, defaults),
		"with no --config the project-local layer is appended last")
}

// newConfigFlagCmd builds a command carrying the same --config flag the root
// command declares, marking it Changed when paths are supplied. Parsing real
// arguments rather than setting Changed by hand keeps the test honest about
// pflag's actual behaviour.
func newConfigFlagCmd(t *testing.T, paths ...string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{Use: "keryx"}

	var cfgPaths []string

	cmd.PersistentFlags().StringArrayVar(&cfgPaths, "config",
		[]string{"/etc/keryx/config.yaml"}, "config files to use")

	args := make([]string, 0, len(paths)*2)
	for _, path := range paths {
		args = append(args, "--config", path)
	}

	require.NoError(t, cmd.ParseFlags(args))

	return cmd
}
