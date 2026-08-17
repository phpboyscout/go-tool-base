package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// A command that only groups subcommands has nothing to implement, so it gets no
// run function of its own — spec 0190. This is the predicate that decides which
// commands those are.
//
// It reads the run function's BODY, not whether main.go exists. Deciding on file
// presence is what made the built binary's exit code depend on the ignore file
// (issue #22), and is the thing this must not reproduce.

func pureGroupFixture(t *testing.T, mainContent string) (*Generator, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"
	cmdDir := filepath.Join(root, "pkg", "cmd", "lexicon")

	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))

	if mainContent != "" {
		require.NoError(t, afero.WriteFile(fs,
			filepath.Join(cmdDir, "main.go"), []byte(mainContent), DefaultFileMode))
	}

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	return New(p, &Config{Path: root, Name: "lexicon"}), cmdDir
}

const (
	untouchedGroupStub = `package lexicon

func RunLexicon(ctx context.Context, props *props.Props, opts *LexiconOptions, args []string) error {
	return errorhandling.ErrRunSubCommand
}
`
	untouchedLeafStub = `package lexicon

func RunLexicon(ctx context.Context, props *props.Props, opts *LexiconOptions, args []string) error {
	return errorhandling.ErrNotImplemented
}
`
	developerWrittenRun = `package lexicon

func RunLexicon(ctx context.Context, props *props.Props, opts *LexiconOptions, args []string) error {
	props.Logger.Info("doing real work")

	return nil
}
`
	// The shape a real consumer arrives at: a hand-written parent that prints its
	// own verb list. Semantically a group, structurally a working command — and
	// the generator cannot tell the difference without guessing at intent.
	handWrittenVerbPrinter = `package lexicon

func RunLexicon(ctx context.Context, props *props.Props, opts *LexiconOptions, args []string) error {
	fmt.Println("Subcommands: list add sync")

	return nil
}
`
	otherFunctionsOnly = `package lexicon

func PreRunLexicon(ctx context.Context, props *props.Props, opts *LexiconOptions, args []string) error {
	return nil
}
`
)

func TestPureGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		hasSubcommands bool
		mainContent    string
		want           bool
		why            string
	}{
		{
			name:           "group with no main.go at all",
			hasSubcommands: true,
			want:           true,
			why:            "nothing defines a run function, so there is none to call",
		},
		{
			name:           "group whose stub still returns ErrRunSubCommand",
			hasSubcommands: true,
			mainContent:    untouchedGroupStub,
			want:           true,
			why:            "the generator wrote that body and the developer has not touched it",
		},
		{
			name:           "group whose stub still returns ErrNotImplemented",
			hasSubcommands: true,
			mainContent:    untouchedLeafStub,
			want:           true,
			why:            "a leaf that gained children: still an untouched stub, either sentinel",
		},
		{
			name:           "group whose main.go defines no run function",
			hasSubcommands: true,
			mainContent:    otherFunctionsOnly,
			want:           true,
			why:            "the developer deleted it; there is nothing to call",
		},
		{
			name:           "group with a hand-written body",
			hasSubcommands: true,
			mainContent:    developerWrittenRun,
			want:           false,
			why:            "a body expresses intent, and the developer owns the outcome",
		},
		{
			name:           "group whose body only prints its verbs",
			hasSubcommands: true,
			mainContent:    handWrittenVerbPrinter,
			want:           false,
			why: "semantically pure, structurally working — pattern-matching a " +
				"printer-shaped body would be guesswork about intent",
		},
		{
			name:           "leaf with no main.go",
			hasSubcommands: false,
			want:           false,
			why:            "a leaf is never a pure group, whatever its stub says",
		},
		{
			name:           "leaf with an untouched stub",
			hasSubcommands: false,
			mainContent:    untouchedLeafStub,
			want:           false,
			why:            "still a leaf: ErrNotImplemented is the right answer for it",
		},
		{
			name:           "group whose main.go does not parse",
			hasSubcommands: true,
			mainContent:    "package lexicon\n\nfunc RunLexicon( {",
			want:           false,
			why:            "fail safe: keep today's wiring rather than silently drop a RunE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := pureGroupFixture(t, tt.mainContent)

			data := templates.CommandData{
				Name:           "lexicon",
				PascalName:     "Lexicon",
				HasSubcommands: tt.hasSubcommands,
			}

			assert.Equal(t, tt.want, g.pureGroup(cmdDir, data), tt.why)
		})
	}
}

// GenerateCommandFile is where the decision has to be made: before cmd.go is
// rendered, because cmd.go is what changes shape as a result. Emission does not
// consume it yet — that is the next change — so this pins the wiring alone.
func TestGenerateCommandFile_ClassifiesThePureGroup(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		mainContent string
		want        bool
	}{
		"no main.go":        {want: true},
		"untouched stub":    {mainContent: untouchedGroupStub, want: true},
		"hand-written body": {mainContent: developerWrittenRun, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, cmdDir := pureGroupFixture(t, tc.mainContent)

			data := &templates.CommandData{
				Package:        "lexicon",
				Name:           "lexicon",
				PascalName:     "Lexicon",
				HasSubcommands: true,
			}

			require.NoError(t, g.GenerateCommandFile(context.Background(), cmdDir, data))
			assert.Equal(t, tc.want, data.PureGroup)
		})
	}
}
