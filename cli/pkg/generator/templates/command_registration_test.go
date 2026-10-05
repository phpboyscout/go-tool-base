package templates

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// renderGoFile renders f and asserts the output parses as Go.
func renderGoFile(t *testing.T, f *jen.File) string {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, f.Render(&buf))

	src := buf.String()
	_, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, parser.ParseComments)
	require.NoError(t, err, src)

	return src
}

func renderRegistration(t *testing.T, data CommandData) string {
	t.Helper()

	if data.Package == "" {
		data.Package = "widget"
		data.PascalName = "Widget"
		data.Name = "widget"
	}

	return renderGoFile(t, CommandRegistration(data))
}

func TestCommandRegistration_FlagDefaults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		flag   CommandFlag
		want   []string
		absent []string
	}{
		{
			name: "duration in hours",
			flag: CommandFlag{Name: "ttl", Type: "duration", Default: "2h"},
			want: []string{"defaultTtl = 2 * time.Hour", "cmd.Flags().DurationVar(&opts.Ttl, \"ttl\", defaultTtl,", "Ttl time.Duration"},
		},
		{
			name: "duration in minutes",
			flag: CommandFlag{Name: "ttl", Type: "duration", Default: "90m"},
			want: []string{"defaultTtl = 90 * time.Minute"},
		},
		{
			name: "duration bare number is seconds",
			flag: CommandFlag{Name: "ttl", Type: "duration", Default: "45"},
			want: []string{"defaultTtl = 45 * time.Second"},
		},
		{
			name: "duration in milliseconds",
			flag: CommandFlag{Name: "ttl", Type: "duration", Default: "1500ms"},
			want: []string{"defaultTtl = 1500 * time.Millisecond"},
		},
		{
			name: "duration sub-millisecond",
			flag: CommandFlag{Name: "ttl", Type: "duration", Default: "1500ns"},
			want: []string{"defaultTtl = time.Duration(1500)"},
		},
		{
			name: "duration unparseable is zero",
			flag: CommandFlag{Name: "ttl", Type: "duration", Default: "soon"},
			want: []string{"defaultTtl = time.Duration(0)"},
		},
		{
			name: "float",
			flag: CommandFlag{Name: "ratio", Type: "float64", Default: "0.5"},
			want: []string{"defaultRatio = float64(0.5)", "Float64Var(&opts.Ratio, \"ratio\", defaultRatio,"},
		},
		{
			name: "int32 range",
			flag: CommandFlag{Name: "port", Type: "int32", Default: "8080"},
			want: []string{"defaultPort = int32(8080)", "Int32Var("},
		},
		{
			name: "int32 overflow falls back to zero",
			flag: CommandFlag{Name: "port", Type: "int32", Default: "99999999999"},
			want: []string{"defaultPort = int32(0)"},
		},
		{
			name: "uint64",
			flag: CommandFlag{Name: "size", Type: "uint64", Default: "7"},
			want: []string{"defaultSize = uint64(7)", "Uint64Var("},
		},
		{
			name: "plain int needs no conversion",
			flag: CommandFlag{Name: "count", Type: "int", Default: "3"},
			want: []string{"defaultCount = 3"},
		},
		{
			name: "bool true",
			flag: CommandFlag{Name: "force", Type: "bool", Default: "true"},
			want: []string{"BoolVar(&opts.Force, \"force\", true,"},
		},
		{
			name: "bool false",
			flag: CommandFlag{Name: "force", Type: "bool", Default: "no"},
			want: []string{"BoolVar(&opts.Force, \"force\", false,"},
		},
		{
			name: "string slice",
			flag: CommandFlag{Name: "tags", Type: "stringSlice", Default: "a, b ,c"},
			want: []string{`StringSliceVar(&opts.Tags, "tags", []string{"a", "b", "c"},`, "Tags []string"},
		},
		{
			name: "string slice empty literal",
			flag: CommandFlag{Name: "tags", Type: "stringSlice", Default: "[]"},
			want: []string{`StringSliceVar(&opts.Tags, "tags", []string{},`},
		},
		{
			name: "int slice",
			flag: CommandFlag{Name: "ids", Type: "intSlice", Default: "1, 2"},
			want: []string{`IntSliceVar(&opts.Ids, "ids", []int{1, 2},`, "Ids []int"},
		},
		{
			name: "int slice empty literal",
			flag: CommandFlag{Name: "ids", Type: "intSlice", Default: "[]"},
			want: []string{`IntSliceVar(&opts.Ids, "ids", []int{},`},
		},
		{
			name:   "default as code is verbatim and makes no constant",
			flag:   CommandFlag{Name: "wait", Type: "duration", Default: "time.Second", DefaultIsCode: true},
			want:   []string{"DurationVar(&opts.Wait, \"wait\", time.Second,"},
			absent: []string{"defaultWait"},
		},
		{
			name:   "string default is a literal",
			flag:   CommandFlag{Name: "mode", Default: "fast"},
			want:   []string{`StringVar(&opts.Mode, "mode", "fast",`},
			absent: []string{"defaultMode"},
		},
		{
			name: "zero values",
			flag: CommandFlag{Name: "ratio", Type: "float64"},
			want: []string{`Float64Var(&opts.Ratio, "ratio", 0.0,`},
		},
		{
			name: "zero bool",
			flag: CommandFlag{Name: "on", Type: "bool"},
			want: []string{`BoolVar(&opts.On, "on", false,`},
		},
		{
			name: "zero duration",
			flag: CommandFlag{Name: "ttl", Type: "duration"},
			want: []string{`DurationVar(&opts.Ttl, "ttl", 0,`},
		},
		{
			name: "zero string slice",
			flag: CommandFlag{Name: "tags", Type: "stringArray"},
			want: []string{`StringArrayVar(&opts.Tags, "tags", []string{},`},
		},
		{
			name: "zero int slice",
			flag: CommandFlag{Name: "ids", Type: "intSlice"},
			want: []string{`IntSliceVar(&opts.Ids, "ids", []int{},`},
		},
		{
			name: "zero string",
			flag: CommandFlag{Name: "mode"},
			want: []string{`StringVar(&opts.Mode, "mode", "",`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := renderRegistration(t, CommandData{Flags: []CommandFlag{tc.flag}})

			for _, w := range tc.want {
				assert.Contains(t, src, w)
			}

			for _, a := range tc.absent {
				assert.NotContains(t, src, a)
			}
		})
	}
}

func TestCommandRegistration_FlagModifiers(t *testing.T) {
	t.Parallel()

	src := renderRegistration(t, CommandData{
		Flags: []CommandFlag{
			{Name: "output", Shorthand: "o", Required: true, Hidden: true, Description: "line one\nline two"},
			{Name: "dup", Persistent: true},
		},
		PersistentFlags: []CommandFlag{
			{Name: "verbose", Type: "bool", Shorthand: "v", Required: true},
		},
		MutuallyExclusive: [][]string{{"output", "verbose"}, {"solo"}},
		RequiredTogether:  [][]string{{"a", "b"}, {"lonely"}},
	})

	assert.Contains(t, src, "cmd.Flags().StringVarP(&opts.Output, \"output\", \"o\", \"\", `line one\nline two`)")
	assert.Contains(t, src, `_ = cmd.MarkFlagRequired("output")`)
	assert.Contains(t, src, `_ = cmd.Flags().MarkHidden("output")`)
	assert.Contains(t, src, `cmd.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false,`)
	assert.Contains(t, src, `_ = cmd.MarkPersistentFlagRequired("verbose")`)
	assert.Contains(t, src, `cmd.MarkFlagsMutuallyExclusive("output", "verbose")`)
	assert.Contains(t, src, `cmd.MarkFlagsRequiredTogether("a", "b")`)
	assert.NotContains(t, src, `"solo"`, "a single-flag group constrains nothing")
	assert.NotContains(t, src, `"lonely"`)
	assert.NotContains(t, src, `"dup"`, "a persistent entry in Flags is registered from PersistentFlags only")
}

func TestCommandRegistration_CommandFields(t *testing.T) {
	t.Parallel()

	src := renderRegistration(t, CommandData{
		WithAssets:       true,
		Aliases:          []string{"w", "wd"},
		Hidden:           true,
		PersistentPreRun: true,
		PreRun:           true,
		Long:             "multi\nline",
		AncestralPersistentFlags: []CommandFlag{
			{Name: "region", Type: "string"},
		},
		MCPHints: setup.MCPHints{
			Title:       "Widget",
			ReadOnly:    new(true),
			Destructive: new(false),
			Idempotent:  new(true),
			OpenWorld:   new(false),
		},
	})

	assert.Contains(t, src, "//go:embed assets/*")
	assert.Contains(t, src, "var assets embed.FS")
	assert.Contains(t, src, `props.Assets.Register("widget", &assets)`)
	assert.Contains(t, src, `Aliases: []string{"w", "wd"}`)
	assert.Contains(t, src, "Hidden:  true")
	assert.Contains(t, src, "Long: `multi\nline`")
	assert.Contains(t, src, "PersistentPreRunE: func(cmd *cobra.Command, args []string) error {")
	assert.Contains(t, src, "cmd.Parent().PersistentPreRunE(cmd, args)")
	assert.Contains(t, src, "return PersistentPreRunWidget(cmd.Context(), props, opts, args)")
	assert.Contains(t, src, "return PreRunWidget(cmd.Context(), props, opts, args)")
	assert.Contains(t, src, `if v, err := cmd.Flags().GetString("region"); err == nil {`)
	assert.Contains(t, src, "opts.Region = v")
	assert.Contains(t, src, "Region string")
	assert.Contains(t, src, `setup.AnnotateMCP(cmd, setup.MCPHints{`)
	assert.Contains(t, src,
		`setup.MCPHints{Title: "Widget", ReadOnly: new(true), Destructive: new(false), Idempotent: new(true), OpenWorld: new(false)}`)
}

func TestCommandRegistration_MCPHintsPartial(t *testing.T) {
	t.Parallel()

	src := renderRegistration(t, CommandData{MCPHints: setup.MCPHints{ReadOnly: new(true)}})

	assert.Contains(t, src, "setup.AnnotateMCP(cmd, setup.MCPHints{ReadOnly: new(true)})")
	assert.NotContains(t, src, "Title")

	assert.NotContains(t, renderRegistration(t, CommandData{}), "AnnotateMCP", "zero hints emit nothing")
}

func TestCommandRegistration_ArgsField(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"NoArgs":           "Args:  cobra.NoArgs,",
		"ExactArgs(2)":     "Args:  cobra.ExactArgs(2),",
		"MinimumNArgs()":   "Args:  cobra.MinimumNArgs(),",
		"MatchAll(nested)": "Args:  cobra.MatchAll(nested),",
	}

	for args, want := range cases {
		t.Run(args, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, renderRegistration(t, CommandData{Args: args}), want)
		})
	}

	assert.NotContains(t, renderRegistration(t, CommandData{}), "Args:", "no args, no field")
}

func TestCommandRegistration_PureGroupWithoutFlagsHasNoOpts(t *testing.T) {
	t.Parallel()

	src := renderRegistration(t, CommandData{PureGroup: true})

	assert.Contains(t, src, "RunE: setup.GroupRunE,")
	assert.NotContains(t, src, "opts :=")
	assert.NotContains(t, src, "RunWidget")
}

func TestCommandConfigValidation(t *testing.T) {
	t.Parallel()

	src := CommandConfigValidation(CommandData{Package: "widget", PascalName: "Widget", Name: "widget"})

	_, err := parser.ParseFile(token.NewFileSet(), "config.go", src, parser.ParseComments)
	require.NoError(t, err, src)

	assert.Contains(t, src, "package widget")
	assert.Contains(t, src, `"gitlab.com/phpboyscout/go/config"`)
	assert.Contains(t, src, "type WidgetConfig struct {")
	assert.Contains(t, src, `config:"widget.api_key"`)
	assert.Contains(t, src, "func ValidateWidgetConfig(cfg config.Reader) error {")
	assert.Contains(t, src, "return config.ValidateStruct[WidgetConfig](cfg)")
}

func TestImportHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `"fmt"`, quoteImport("fmt"))
	assert.Equal(t, `alias "x/y"`, quoteImport(`alias "x/y"`))

	assert.Equal(t, groupThirdParty, importGroup(`alias "github.com/a/b"`, ""))
	assert.Equal(t, groupLocal, importGroup("example.com/me/pkg/x", "example.com/me"))
	assert.Equal(t, groupStd, importGroup("strings", "example.com/me"))

	for _, redundant := range []string{"io", "os", "fmt", "context", "example.com/x/props", "github.com/phpboyscout/logger", "github.com/charmbracelet/log", "charm.land/log/v2"} {
		assert.Truef(t, isRedundantImport(redundant), "%s is redundant", redundant)
	}

	assert.False(t, isRedundantImport("strings"))
}

func TestIntBitSize(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 32, intBitSize("uint32"))
	assert.Equal(t, 64, intBitSize("uint"))
	assert.Equal(t, 64, intBitSize("int8"), "unknown sizes parse at 64 bits")
}

func TestGetConstantValue_NonNumericKindsHaveNone(t *testing.T) {
	t.Parallel()

	for _, typ := range []string{"string", "bool", "stringSlice", "intSlice"} {
		assert.Nilf(t, getConstantValue(CommandFlag{Type: typ, Default: "x"}), "%s has no constant", typ)
	}

	_, ok := getConstantForFlag(CommandFlag{Name: "tags", Type: "stringSlice", Default: "a"})
	assert.False(t, ok)

	assert.Contains(t, renderStatement(t, jen.Id("n").Op("=").Add(getIntConstantValue(CommandFlag{Name: "n", Default: "4"}))), "n = 4",
		"an untyped flag is an int and needs no conversion")
	assert.Zero(t, parseDuration(""))
}

func TestCommandExecution_ContentSources(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "package x\n", CommandExecution(CommandData{FullFileContent: "package x\n", Logic: "ignored"}),
		"AI-supplied full content wins verbatim")

	src := CommandExecution(CommandData{
		Package:    "widget",
		PascalName: "Widget",
		Name:       "widget",
		Logic:      "\tprops.Logger.Info(\"hi\")\n\treturn nil",
		Imports:    []string{"strings", "fmt", "strings"},
	})

	_, err := parser.ParseFile(token.NewFileSet(), "main.go", src, 0)
	require.NoError(t, err, src)
	assert.Contains(t, src, `props.Logger.Info("hi")`)
	assert.NotContains(t, src, "ErrNotImplemented")
	assert.Equal(t, 1, strings.Count(src, `"strings"`), "a repeated import is written once")
	assert.NotContains(t, src, `"fmt"`, "redundant imports are dropped")
}

func TestUnknownInputsFallBackSafely(t *testing.T) {
	t.Parallel()

	assert.Equal(t, flagTypes["string"], specFor("complex128"), "an unknown flag type renders as a string")
	assert.Contains(t, renderStatement(t, jen.Id("x").Op("=").Add(ExternalArgExpr("unknown"))), "x = p",
		"an unknown injection token renders as p")
}
