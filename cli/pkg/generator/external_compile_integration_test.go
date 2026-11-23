package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// An attached command's alias that the root, or another file beside it,
// declares left a root that did not compile (#228).
func TestGeneratedAttachedCommandCompiles(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	const framework = "gitlab.com/phpboyscout/go-tool-base"

	path := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}

	newGenerator := func() *Generator {
		g := New(p, &Config{Path: path})
		g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

		return g
	}

	require.NoError(t, newGenerator().GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "attach-tool", Repo: "test/attach-tool", Host: "github.com", Description: "d", Path: path,
		Features: []ManifestFeature{{Name: "changelog", Enabled: false}, {Name: "docs", Enabled: false}},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(path, "pkg", "cmd", "root", "publish.go"),
		[]byte("package root\n\nfunc publish() string { return \"\" }\n\nvar _ = publish()\n"), 0o600))

	injectGoToolBaseReplace(t, path, localGoToolBasePath(t))
	runGo(t, path, "mod", "tidy")

	for _, alias := range []string{"tool", "p", "v", "l", "assets", "init", "NewCmdRoot", "publish", "err", "props", "type", "release"} {
		require.NoErrorf(t, newGenerator().AttachExternalCommand(context.Background(), ExternalCommandSpec{
			Module: framework, ImportPath: framework + "/pkg/cmd/doctor", Version: "v0.53.0", Alias: alias,
			Attach: []ManifestExternalAttach{{Constructor: "NewCmdDoctor", Args: []string{"props"}}},
		}), "attach as %s", alias)

		t.Logf("building with alias %s", alias)
		runGo(t, path, "build", "-buildvcs=false", "./pkg/cmd/root")
		require.NoError(t, newGenerator().DetachExternalCommand(context.Background(), framework))
	}
}
