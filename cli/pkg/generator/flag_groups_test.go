package generator

import (
	"context"
	"regexp"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	droppedGroup  = "dropped a recorded flag group, too few of its flags are left"
	narrowedGroup = "took the flags the command no longer has out of a recorded flag group"
	renderedGroup = "cmd.go leaves out of a flag group the flags the command does not have"
)

var (
	recordedExclusive = [][]string{{"csv", "xml"}, {"json", "yaml"}, {"json", "toml", "yaml"}}
	recordedTogether  = [][]string{{"yaml", "csv"}, {"csv", "yaml", "toml"}, {"json", "xml"}}
)

func TestGenerate_AgainTakesARemovedFlagOutOfItsGroups(t *testing.T) {
	t.Parallel()

	const path = "/work"

	for parent, dir := range map[string]string{"root": "deploy", "ops": "ops/deploy"} {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			p := setupTestProject(t, path)

			if parent != "root" {
				generateCmd(t, p, path, parent, "root")
			}

			generate := func(flags ...string) []logger.Entry {
				log := logger.NewBuffer()
				p.Logger = log

				require.NoError(t, New(p, &Config{
					Path: path, Name: "deploy", Parent: parent, Short: "d", Force: true, Flags: flags,
				}).Generate(context.Background()))

				return log.Entries()
			}
			generate("json:bool:J", "yaml:bool:Y", "toml:bool:T:true", "csv:bool:C", "xml:bool:X")

			g := New(p, &Config{Path: path, Name: "deploy", Parent: parent})
			groupFlags(t, g, recordedExclusive, recordedTogether)

			entries := generate()
			cmd, err := g.findManifestCommand()
			require.NoError(t, err)
			assert.Equal(t, recordedExclusive, cmd.MutuallyExclusive, "flags not stated")
			assert.Equal(t, recordedTogether, cmd.RequiredTogether, "flags not stated")
			assert.Empty(t, groupLines(entries, droppedGroup, narrowedGroup, renderedGroup))

			entries = generate("json:bool:J", "toml:bool:T:true", "csv:bool:C", "xml:bool:X")
			cmd, err = g.findManifestCommand()
			require.NoError(t, err)
			assert.Equal(t, [][]string{{"csv", "xml"}, {"json", "toml"}}, cmd.MutuallyExclusive)
			assert.Equal(t, [][]string{{"csv", "toml"}, {"json", "xml"}}, cmd.RequiredTogether)

			assert.Equal(t, []string{
				`cmd.MarkFlagsMutuallyExclusive("csv", "xml")`,
				`cmd.MarkFlagsMutuallyExclusive("json", "toml")`,
				`cmd.MarkFlagsRequiredTogether("csv", "toml")`,
				`cmd.MarkFlagsRequiredTogether("json", "xml")`,
			}, markedGroups(t, p.FS, path+"/pkg/cmd/"+dir+"/cmd.go"))

			assert.Equal(t, [][]any{
				{"command", "deploy", "group", "mutually_exclusive", "before", []string{"json", "yaml"}},
				{"command", "deploy", "group", "required_together", "before", []string{"yaml", "csv"}},
			}, groupLines(entries, droppedGroup))
			assert.Equal(t, [][]any{
				{"command", "deploy", "group", "mutually_exclusive", "before", []string{"json", "toml", "yaml"}, "after", []string{"json", "toml"}},
				{"command", "deploy", "group", "required_together", "before", []string{"csv", "yaml", "toml"}, "after", []string{"csv", "toml"}},
			}, groupLines(entries, narrowedGroup))
		})
	}
}

func TestGenerate_AgainWithOneFlagDropsItsGroups(t *testing.T) {
	t.Parallel()

	const path = "/work"

	p := setupTestProject(t, path)
	log := logger.NewBuffer()

	generate := func(flags ...string) {
		require.NoError(t, New(p, &Config{
			Path: path, Name: "deploy", Parent: "root", Short: "d", Force: true, Flags: flags,
		}).Generate(context.Background()))
	}
	generate("json:bool:J", "yaml:bool:Y", "toml:bool:T")

	g := New(p, &Config{Path: path, Name: "deploy", Parent: "root"})
	groupFlags(t, g, [][]string{{"json", "yaml"}}, [][]string{{"json", "toml"}})

	p.Logger = log
	generate("json:bool:J")

	cmd, err := g.findManifestCommand()
	require.NoError(t, err)
	assert.Empty(t, cmd.MutuallyExclusive)
	assert.Empty(t, cmd.RequiredTogether)
	assert.Empty(t, markedGroups(t, p.FS, path+"/pkg/cmd/deploy/cmd.go"))
	assert.Equal(t, [][]any{
		{"command", "deploy", "group", "mutually_exclusive", "before", []string{"json", "yaml"}},
		{"command", "deploy", "group", "required_together", "before", []string{"json", "toml"}},
	}, groupLines(log.Entries(), droppedGroup))
}

func TestRegenerateProject_RendersNoGroupWithAMissingFlag(t *testing.T) {
	t.Parallel()

	const path = "/work"

	p := setupTestProject(t, path)
	require.NoError(t, New(p, &Config{
		Path: path, Name: "deploy", Parent: "root", Short: "d", Force: true,
		Flags: []string{"json:bool:J", "toml:bool:T:true", "csv:bool:C", "xml:bool:X"},
	}).Generate(context.Background()))

	g := New(p, &Config{Path: path, Name: "deploy", Parent: "root"})
	groupFlags(t, g, recordedExclusive, recordedTogether)

	log := logger.NewBuffer()
	p.Logger = log
	require.NoError(t, New(p, &Config{Path: path, Force: true}).RegenerateProject(context.Background()))

	assert.Equal(t, []string{
		`cmd.MarkFlagsMutuallyExclusive("csv", "xml")`,
		`cmd.MarkFlagsMutuallyExclusive("json", "toml")`,
		`cmd.MarkFlagsRequiredTogether("csv", "toml")`,
		`cmd.MarkFlagsRequiredTogether("json", "xml")`,
	}, markedGroups(t, p.FS, path+"/pkg/cmd/deploy/cmd.go"))

	cmd, err := g.findManifestCommand()
	require.NoError(t, err)
	assert.Equal(t, recordedExclusive, cmd.MutuallyExclusive, "a regenerate does not edit the author's groups")
	assert.Equal(t, recordedTogether, cmd.RequiredTogether, "a regenerate does not edit the author's groups")

	assert.Empty(t, groupLines(log.Entries(), droppedGroup, narrowedGroup))
	assert.Equal(t, [][]any{
		{"command", "deploy", "group", "mutually_exclusive", "recorded", []string{"json", "yaml"}, "rendered", []string(nil)},
		{"command", "deploy", "group", "mutually_exclusive", "recorded", []string{"json", "toml", "yaml"}, "rendered", []string{"json", "toml"}},
		{"command", "deploy", "group", "required_together", "recorded", []string{"yaml", "csv"}, "rendered", []string(nil)},
		{"command", "deploy", "group", "required_together", "recorded", []string{"csv", "yaml", "toml"}, "rendered", []string{"csv", "toml"}},
	}, groupLines(log.Entries(), renderedGroup))
}

func TestRegenerateProject_SaysNothingOfGroupsInAKeptFile(t *testing.T) {
	t.Parallel()

	const path = "/work"

	p := setupTestProject(t, path)
	require.NoError(t, New(p, &Config{
		Path: path, Name: "deploy", Parent: "root", Short: "d", Force: true, Flags: []string{"json:bool:J"},
	}).Generate(context.Background()))

	groupFlags(t, New(p, &Config{Path: path, Name: "deploy", Parent: "root"}), recordedExclusive, recordedTogether)
	require.NoError(t, afero.WriteFile(p.FS, path+"/.gtb/ignore", []byte("pkg/cmd/deploy/cmd.go\n"), 0o644))

	log := logger.NewBuffer()
	p.Logger = log
	require.NoError(t, New(p, &Config{Path: path, Force: true}).RegenerateProject(context.Background()))

	assert.True(t, log.Contains("Keeping registration file"))
	assert.Empty(t, groupLines(log.Entries(), renderedGroup))
}

func TestDeclaredGroups(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{"json": true, "toml": true}
	tests := map[string]struct {
		group, want []string
		edited      bool
	}{
		"nothing missing":           {[]string{"json", "toml"}, []string{"json", "toml"}, false},
		"one name, declared":        {[]string{"json"}, []string{"json"}, false},
		"one name, missing":         {[]string{"yaml"}, nil, true},
		"one flag named twice left": {[]string{"json", "json", "yaml"}, nil, true},
		"order and duplicates stay": {[]string{"toml", "toml", "yaml", "json"}, []string{"toml", "toml", "json"}, true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			before := append([]string{}, tc.group...)
			got, edits := declaredGroups("k", [][]string{tc.group}, declared)
			assert.Equal(t, before, tc.group, "the recorded group is left alone")

			if tc.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, [][]string{tc.want}, got)
			}

			if tc.edited {
				assert.Equal(t, []groupEdit{{Kind: "k", Before: before, After: tc.want}}, edits)
			} else {
				assert.Empty(t, edits)
			}
		})
	}
}

func groupFlags(t *testing.T, g *Generator, exclusive, together [][]string) {
	t.Helper()

	m, err := g.loadManifest()
	require.NoError(t, err)

	cmd := walkCommandPath(m.Commands, joinCommandPath(g.getParentPathParts(), g.config.Name))
	cmd.MutuallyExclusive = exclusive
	cmd.RequiredTogether = together
	require.NoError(t, g.marshalManifestFile(ManifestPathFor(g.config.Path), m))
}

// groupLines returns the fields of each warning carrying one of the messages.
func groupLines(entries []logger.Entry, messages ...string) [][]any {
	var lines [][]any

	for _, e := range entries {
		for _, m := range messages {
			if e.Level == logger.WarnLevel && e.Message == m {
				lines = append(lines, e.Keyvals)
			}
		}
	}

	return lines
}

func markedGroups(t *testing.T, fs afero.Fs, file string) []string {
	t.Helper()

	src, err := afero.ReadFile(fs, file)
	require.NoError(t, err)

	return regexp.MustCompile(`cmd\.MarkFlags(?:MutuallyExclusive|RequiredTogether)\(.*\)`).FindAllString(string(src), -1)
}
