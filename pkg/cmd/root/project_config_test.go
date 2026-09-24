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
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// findProject is FindProjectConfig with only YAML linked.
func findProject(t *testing.T, fs afero.Fs, tool, dir string) string {
	t.Helper()

	got, err := setup.FindProjectConfig(fs, tool, dir, nil)
	require.NoError(t, err)

	return got
}

func TestDiscoverProjectConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	// a repo with the project config at its root, and a nested working dir
	_ = fs.MkdirAll("/repo/sub/deep", 0o755)
	_ = afero.WriteFile(fs, "/repo/.keryx.yaml", []byte("themes: {}\n"), 0o644)

	// walks up from a nested dir to the repo-root config
	if got := findProject(t, fs, "keryx", "/repo/sub/deep"); got != "/repo/.keryx.yaml" {
		t.Errorf("nested cwd: got %q, want /repo/.keryx.yaml", got)
	}

	// found at the dir itself
	if got := findProject(t, fs, "keryx", "/repo"); got != "/repo/.keryx.yaml" {
		t.Errorf("repo root: got %q", got)
	}

	// a different tool name → not matched
	if got := findProject(t, fs, "othertool", "/repo/sub"); got != "" {
		t.Errorf("wrong tool name should not match: %q", got)
	}

	// no config anywhere above → ""
	_ = fs.MkdirAll("/elsewhere", 0o755)
	if got := findProject(t, fs, "keryx", "/elsewhere"); got != "" {
		t.Errorf("absent: got %q, want \"\"", got)
	}

	// empty inputs
	if findProject(t, fs, "", "/repo") != "" || findProject(t, fs, "keryx", "") != "" {
		t.Error("empty tool/dir should return \"\"")
	}

}

// TestProjectConfigLayer_ExplicitConfigSuppressesTheProjectLayer pins the rule
// that an explicitly named --config is authoritative.
//
// The flag is declared as a StringArray whose default value IS the standard
// paths, so supplying it replaces them outright rather than adding to them —
// naming a config file means "use this one". Layering a .<tool>.yaml the caller
// never named on top of that, as this once did, let it override the file they
// did name. The failure was silent: the tool ran against different settings with
// nothing on the command line to account for it.
//
// Both directions matter, so both are asserted here.
func TestProjectConfigLayer_ExplicitConfigSuppressesTheProjectLayer(t *testing.T) {
	// Not parallel: t.Chdir is process-wide, and projectConfigLayer reads the
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

	withFlag := newConfigFlagCmd(t, "/my/exact/file.yaml")
	got, err := projectConfigLayer(props, withFlag)
	require.NoError(t, err)
	assert.Empty(t, got, "an explicit --config must suppress the project-local layer entirely")

	// Without the flag the layer is discovered from the working directory.
	withoutFlag := newConfigFlagCmd(t)
	got, err = projectConfigLayer(props, withoutFlag)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".keryx.yaml"), got, "with no --config the project-local layer is discovered")
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
