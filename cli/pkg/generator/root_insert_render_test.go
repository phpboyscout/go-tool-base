package generator

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/dave/dst/decorator"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// generate command and regenerate project write the root call the same way,
// so a regenerate straight after a generate changes nothing (#147).
func TestInsertIntoRootCall_MatchesTheSkeletonRender(t *testing.T) {
	t.Parallel()

	sub := func(name string) templates.SkeletonSubcommand {
		return templates.SkeletonSubcommand{
			ImportPath: "example.com/acme/widget/pkg/cmd/" + name, PkgAlias: name, Constructor: "NewCmd" + PascalCase(name),
		}
	}

	for _, adapter := range []bool{false, true} {
		t.Run(fmt.Sprintf("external adapter %v", adapter), func(t *testing.T) {
			t.Parallel()

			render := func(subs ...templates.SkeletonSubcommand) string {
				var buf bytes.Buffer

				require.NoError(t, templates.SkeletonRoot(templates.SkeletonRootData{
					Name: "widget", ModulePath: "example.com/acme/widget", ExternalAdapter: adapter, Subcommands: subs,
				}).Render(&buf))

				return buf.String()
			}

			f, err := decorator.Parse(render(sub("hello")))
			require.NoError(t, err)

			fn := findFuncDecl(f, "NewCmdRoot")
			require.NotNil(t, fn)

			(&Generator{}).appendSubcommandCallToRootInit(fn, &subcommandContext{pkgName: "world", funcNameToBeCalled: "NewCmdWorld"})

			var inserted bytes.Buffer
			require.NoError(t, decorator.Fprint(&inserted, f))

			require.Equal(t, rootCmdLine(t, render(sub("hello"), sub("world"))), rootCmdLine(t, inserted.String()))
		})
	}
}

func rootCmdLine(t *testing.T, src string) string {
	t.Helper()

	start := strings.Index(src, "rootCmd := ")
	require.GreaterOrEqual(t, start, 0, src)

	end := strings.Index(src[start:], "\n\n")
	require.Positive(t, end, src)

	return src[start : start+end]
}
