package generator

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const hookedSignMain = `package sign

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func FlagsSign(_ *props.Props, flags *pflag.FlagSet) {
	flags.String("kms-region", "eu-west-1", "KMS region")
	flags.Lookup("backend").Usage = "one of: kms"
}

func RunSign(_ context.Context, props *props.Props, opts *SignOptions, _ []string) error {
	region, err := opts.FlagSet.GetString("kms-region")
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(props.GetIO().Out(), opts.Backend, region, opts.FlagSet.Changed("kms-region"))

	return err
}
`

// TestGeneratedFlagsHookCompiles is spec 0224's build check: a hook-added flag
// reaches the run, help and the MCP schema, and a pure group whose only
// function is the hook builds with its main.go absent or preserved.
func TestGeneratedFlagsHookCompiles(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	path := t.TempDir()
	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}
	hooked := func(c *Config) { c.FlagsHook = true }

	g := New(p, &Config{Path: path})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "hook-tool", Repo: "test/hook-tool", Host: "github.com", Description: "d", Path: path,
	}))

	addCmd(t, p, path, "sign", "root", hooked, func(c *Config) { c.Flags = []string{"backend:string:Backend:false::false:local"} })
	addCmd(t, p, path, "preserved", "root", hooked)
	addCmd(t, p, path, "child", "preserved")
	addCmd(t, p, path, "fresh", "root", hooked)
	addCmd(t, p, path, "child", "fresh")
	require.NoError(t, os.WriteFile(filepath.Join(path, "pkg", "cmd", "sign", "main.go"), []byte(hookedSignMain), 0o600))

	freshMain := filepath.Join(path, "pkg", "cmd", "fresh", "main.go")
	require.NoError(t, os.Remove(freshMain))

	injectGoToolBaseReplace(t, path, localGoToolBasePath(t))

	regen := New(p, &Config{Path: path})
	regen.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	require.NoError(t, regen.RegenerateProject(context.Background()))

	src, err := os.ReadFile(freshMain)
	require.NoError(t, err)
	assert.NotContains(t, string(src), "RunFresh", "a pure group's main.go holds the hook alone")

	runGo(t, path, "mod", "tidy")

	bin := filepath.Join(t.TempDir(), "hook-tool")
	runGo(t, path, "build", "-buildvcs=false", "-o", bin, "./cmd/hook-tool")
	runGo(t, path, "vet", "./...")

	work := t.TempDir()
	runHookTool := newHookToolRunner(t, bin, work)
	runHookTool("init")

	assert.Contains(t, runHookTool("sign", "--help"), "one of: kms")
	assert.Contains(t, runHookTool("sign"), "local eu-west-1 false")
	assert.Contains(t, runHookTool("sign", "--kms-region", "us-east-1"), "local us-east-1 true")
	assert.Contains(t, runHookTool("sign", "--kms-region", "eu-west-1"), "local eu-west-1 true")

	runHookTool("mcp", "tools")

	assert.Contains(t, string(mcpToolSchema(t, filepath.Join(work, "mcp-tools.json"), "hook-tool_sign")), `"kms-region"`)
}

func newHookToolRunner(t *testing.T, bin, work string) func(args ...string) string {
	t.Helper()

	env := []string{"HOME=" + t.TempDir(), "PATH=" + os.Getenv("PATH")}

	return func(args ...string) string {
		cmd := exec.Command(bin, args...)
		cmd.Dir = work
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "%v:\n%s", args, out)

		return string(out)
	}
}

func mcpToolSchema(t *testing.T, path, name string) json.RawMessage {
	t.Helper()

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var tools []struct {
		Name        string          `json:"name"`
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	require.NoError(t, json.Unmarshal(raw, &tools))

	for _, tool := range tools {
		if tool.Name == name {
			return tool.InputSchema
		}
	}

	t.Fatalf("no tool %s in %s", name, raw)

	return nil
}
