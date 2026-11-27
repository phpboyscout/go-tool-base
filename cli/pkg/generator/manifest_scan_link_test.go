package generator

import (
	"context"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
)

// A command registered from another subtree came back from its own directory's
// scan unlinked and was dropped as an orphan, so the next regenerate project
// took it out of the tool (#256). The manifest cannot say where it lives, so the
// run stops and names it.
func TestRegenerateManifest_StopsAtARegistrationFromAnotherSubtree(t *testing.T) {
	t.Parallel()

	for name, kid := range map[string]string{
		"a nested package": registeringSource("hook/kid", "hook/kid/before", "lib/one"),
		"a hidden package": registeringSource("hook/kid", "hook/kid/before", "_deep"),
		"a cycle back up":  registeringSource("hook/kid", "_deep"),
		"after a sibling":  registeringSource("hook/kid", "hook/kid/before", "lib/one", "hook/kid/after"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, map[string]string{
				"hook/kid": kid,
				"_deep":    registeringSource("_deep", "hook", "hook/kid"),
			})

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandOutsideParent)
			require.ErrorContains(t, err, "/work/pkg/cmd/hook/kid registers /work/pkg/cmd/")

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, unreadableManifest, raw, "the manifest has not been changed")
		})
	}
}

// hook may register lib/one and places it, so the scan linked lib/one there
// and kid's own registration of it vanished from the manifest.
func TestRegenerateManifest_StopsAtASecondRegistrationOfAPlacedCommand(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{
		"hook":     registeringSource("hook", "hook/kid", "lib/one"),
		"hook/kid": registeringSource("hook/kid", "hook/kid/before", "lib/one"),
	})

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	require.ErrorContains(t, err, "/work/pkg/cmd/hook/kid registers /work/pkg/cmd/lib/one, which /work/pkg/cmd/hook registers")
	assert.Contains(t, errors.FlattenHints(err), "neither registration matches. Keep one and move the package to match it: "+
		"to /work/pkg/cmd/hook/one to keep /work/pkg/cmd/hook's, or to /work/pkg/cmd/hook/kid/one to keep /work/pkg/cmd/hook/kid's")

	raw, _ := readRoundTripManifest(t, fs)
	assert.Equal(t, unreadableManifest, raw, "the manifest has not been changed")
}

// A registration from above of a package a deeper command already holds is
// inside the registrant's directory, so the error names the other holder
// rather than telling the user to move a package that is already in place.
func TestRegenerateManifest_NamesTheCommandAlreadyHoldingAPackage(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{
		"hook":     registeringSource("hook", "hook/kid", "hook/kid/before"),
		"hook/kid": registeringSource("hook/kid", "hook/kid/before"),
	})

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	require.ErrorContains(t, err, "/work/pkg/cmd/hook registers /work/pkg/cmd/hook/kid/before, which /work/pkg/cmd/hook/kid registers")
	assert.Contains(t, errors.FlattenHints(err), "beneath /work/pkg/cmd/hook/kid, so keep that registration and remove the one in /work/pkg/cmd/hook.")

	raw, _ := readRoundTripManifest(t, fs)
	assert.Equal(t, unreadableManifest, raw, "the manifest has not been changed")
}

// Nothing the root reaches registers aside, so its claim on lib/one is no
// registration in the tool, and both are left out with the orphan warning.
func TestRegenerateManifest_LeavesOutWhatOnlyAnOrphanRegisters(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{
		"hook/kid":    registeringSource("hook/kid", "hook/kid/before"),
		"aside":       registeringSource("aside", "aside/inner"),
		"aside/inner": registeringSource("aside/inner", "lib/one"),
	})

	require.NoError(t, g.RegenerateManifest(context.Background()))

	_, m := readRoundTripManifest(t, fs)
	require.Len(t, m.Commands, 1)
	assert.Equal(t, "hook", m.Commands[0].Name)
}

// subtreeProject is a project whose root registers hook, hook registers
// hook/kid, and lib/one registers lib/two, with sources overriding any of them.
func subtreeProject(t *testing.T, sources map[string]string) (afero.Fs, *Generator) {
	t.Helper()

	fs, g := roundTripProject(t, "approve", approveSource, unreadableManifest)
	require.NoError(t, fs.RemoveAll("/work/pkg/cmd/approve"))

	all := map[string]string{
		"hook":            registeringSource("hook", "hook/kid"),
		"hook/kid":        registeringSource("hook/kid"),
		"hook/kid/before": registeringSource("hook/kid/before"),
		"hook/kid/after":  registeringSource("hook/kid/after"),
		"lib/one":         registeringSource("lib/one", "lib/two"),
		"lib/two":         registeringSource("lib/two"),
		"_deep":           registeringSource("_deep"),
	}
	for dir, source := range sources {
		all[dir] = source
	}

	for dir, source := range all {
		require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/"+dir+"/cmd.go", []byte(source), 0o644))
	}

	root := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/hook"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, hook.NewCmdHook(p))
}`
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte(root), 0o644))

	return fs, g
}

// regenerate project scaffolds each command at pkg/cmd/<parent commands>/<name>,
// so a manifest recording one anywhere else is one that run breaks (#262).
func TestRegenerateManifest_RefusesACommandOutsideItsPlace(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		sources map[string]string
		hint    string
	}{
		"a top-level command registering another subtree": {map[string]string{
			"hook":     registeringSource("hook", "hook/kid", "lib/one"),
			"hook/kid": registeringSource("hook/kid", "hook/kid/before"),
		}, "stub this one at /work/pkg/cmd/hook/one. Move /work/pkg/cmd/lib/one there, and update the registration in /work/pkg/cmd/hook."},
		"a sibling under the directory holding the registrant": {map[string]string{
			"hook/kid":      registeringSource("hook/kid", "hook/kid/before", "hook/side/one"),
			"hook/side/one": registeringSource("hook/side/one"),
		}, "stub this one at /work/pkg/cmd/hook/kid/one. Move /work/pkg/cmd/hook/side/one there, and update the registration in /work/pkg/cmd/hook/kid."},
		"the root registering a nested package": {map[string]string{
			"hook": registeringSource("hook", "hook/kid", "hook/kid/after"),
		}, "stub this one at /work/pkg/cmd/hook/after."},
		"a command under another name than its directory": {map[string]string{
			"hook/kid": strings.Replace(registeringSource("hook/kid", "hook/kid/before"), `Use: "kid"`, `Use: "child"`, 1),
		}, "stub this one at /work/pkg/cmd/hook/child. Move /work/pkg/cmd/hook/kid there with its constructor renamed " +
			`from NewCmdKid to NewCmdChild, and update the registration in /work/pkg/cmd/hook. In /work/pkg/cmd/hook/kid, also rename ` +
			`whichever of these it has to match "child": the package clause to package child, the options type to ChildOptions, ` +
			`the run function to RunChild, and the setup.Wrap name. Or set the name in its Use back to "kid"`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, tc.sources)
			before, _ := readRoundTripManifest(t, fs)

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandMisplaced)
			assert.Contains(t, errors.FlattenHints(err), tc.hint)

			if !strings.Contains(tc.hint, "Use back") {
				assert.NotContains(t, errors.FlattenHints(err), "Use back", "a Use that names its own directory does not keep it in place")
			}

			after, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, before, after, "nothing is written")
		})
	}
}

// Two commands in one directory registering one package is one registration
// too many, and the one the parent already places right is the one to keep:
// moving the package for the other would break the first (#262).
func TestRegenerateManifest_NamesBothRegistrationsOfOnePackage(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{
		"hook":     registeringSource("hook", "hook/kid", "hook/one"),
		"hook/kid": registeringSource("hook/kid", "hook/kid/before", "hook/one"),
		"hook/one": registeringSource("hook/one"),
	})
	before, _ := readRoundTripManifest(t, fs)

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	assert.Contains(t, errors.FlattenHints(err),
		"regenerate project puts /work/pkg/cmd/hook/one beneath /work/pkg/cmd/hook, so keep that registration and remove the one in /work/pkg/cmd/hook/kid.")

	after, _ := readRoundTripManifest(t, fs)
	assert.Equal(t, before, after, "nothing is written")
}

// Two commands of one name beneath one parent both want the same directory, so
// moving either there is no answer. The hint asks for a rename instead.
func TestRegenerateManifest_RefusesTwoCommandsOfOneName(t *testing.T) {
	t.Parallel()

	const otherKid = `package kid
import "github.com/spf13/cobra"
func NewCmdOther() *cobra.Command {
	return &cobra.Command{Use: "kid"}
}`

	for name, tc := range map[string]struct {
		sources map[string]string
		other   bool
	}{
		"in two directories": {map[string]string{
			"hook":      registeringSource("hook", "hook/kid2", "hook/kid"),
			"hook/kid":  registeringSource("hook/kid"),
			"hook/kid2": strings.Replace(registeringSource("hook/kid2"), `Use: "kid2"`, `Use: "kid"`, 1),
		}, false},
		"in one directory": {map[string]string{
			"hook":     strings.Replace(registeringSource("hook", "hook/kid", "hook/kid"), "cb.NewCmdKid(props)", "cb.NewCmdOther()", 1),
			"hook/kid": registeringSource("hook/kid"),
		}, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, tc.sources)
			if tc.other {
				require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/kid/other.go", []byte(otherKid), 0o644))
			}

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandMisplaced)
			assert.Contains(t, errors.FlattenHints(err),
				`regenerate project puts it at /work/pkg/cmd/hook/kid, which already holds another command named "kid". Rename one of them`)
		})
	}
}

// The hint sends the package to where regenerate project puts it, beneath the
// registrant, not merely somewhere the scan would link it.
func TestRegenerateManifest_HintsWhereRegenerateProjectPutsTheCommand(t *testing.T) {
	t.Parallel()

	_, g := subtreeProject(t, map[string]string{
		"hook/kid": registeringSource("hook/kid", "lib/one"),
	})

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	assert.Contains(t, errors.FlattenHints(err), "Move /work/pkg/cmd/lib/one to /work/pkg/cmd/hook/kid/one")
}

// Removing the wrong one of two registrations used to leave a manifest that
// regenerate project then broke, so the hint names the one to keep, here the
// command whose directory holds the package, though the root holds it too.
func TestRegenerateManifest_KeepsTheRegistrationRegenerateProjectPlacesBeneath(t *testing.T) {
	t.Parallel()

	_, g := subtreeProject(t, map[string]string{
		"hook/kid": registeringSource("hook/kid"),
	})
	root := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/hook"
	"test-tool/pkg/cmd/hook/kid"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, hook.NewCmdHook(p), kid.NewCmdKid(p))
}`
	require.NoError(t, afero.WriteFile(g.props.FS, "/work/pkg/cmd/root/cmd.go", []byte(root), 0o644))

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	assert.Contains(t, errors.FlattenHints(err), "beneath /work/pkg/cmd/hook, so keep that registration and remove the one in /work/pkg/cmd/root.")
}

// The root's commands sit in pkg/cmd, not beside the root in pkg/cmd/root, so
// the move target for keeping the root's registration is pkg/cmd/<name>.
func TestRegenerateManifest_MovesARootRegistrationIntoPkgCmd(t *testing.T) {
	t.Parallel()

	_, g := subtreeProject(t, map[string]string{
		"hook/kid": registeringSource("hook/kid", "lib/one"),
	})
	root := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/hook"
	"test-tool/pkg/cmd/lib/one"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, hook.NewCmdHook(p), one.NewCmdOne(p))
}`
	require.NoError(t, afero.WriteFile(g.props.FS, "/work/pkg/cmd/root/cmd.go", []byte(root), 0o644))

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	assert.Contains(t, errors.FlattenHints(err), "to /work/pkg/cmd/one to keep /work/pkg/cmd/root's")
}

// A registration only an orphan holds is not one the tool keeps, so the hint
// moves the package beneath the reachable registrant rather than naming the
// orphan's registration to keep, which would drop the command.
func TestRegenerateManifest_DoesNotKeepAnOrphansRegistration(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		sources map[string]string
		hint    string
	}{
		"the orphan holds it in its own directory": {map[string]string{
			"aside":   registeringSource("aside", "aside/x"),
			"aside/x": registeringSource("aside/x"),
			"hook":    registeringSource("hook", "hook/kid", "aside/x"),
		}, "Move /work/pkg/cmd/aside/x to /work/pkg/cmd/hook/x"},
		"neither registration matches": {map[string]string{
			"aside":    registeringSource("aside", "lib/one"),
			"hook/kid": registeringSource("hook/kid", "lib/one"),
		}, "Move /work/pkg/cmd/lib/one to /work/pkg/cmd/hook/kid/one"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, g := subtreeProject(t, tc.sources)

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandOutsideParent)
			assert.Contains(t, errors.FlattenHints(err), tc.hint)
			assert.NotContains(t, errors.FlattenHints(err), "aside's")
		})
	}
}

// An orphan inside a package's own directory can claim it before the command
// regenerate project puts it beneath. That registration is the one to keep.
func TestRegenerateManifest_KeepsTheReachableRegistrationOverAnOrphanClaim(t *testing.T) {
	t.Parallel()

	_, g := subtreeProject(t, map[string]string{
		"hook":           registeringSource("hook", "hook/kid", "hook/one"),
		"hook/one":       registeringSource("hook/one"),
		"hook/one/aside": registeringSource("hook/one/aside", "hook/one"),
	})

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandOutsideParent)
	assert.Contains(t, errors.FlattenHints(err),
		"beneath /work/pkg/cmd/hook, so keep that registration and remove the one in /work/pkg/cmd/hook/one/aside.")
}

// ring and loop register each other, and the top-level scan links each under
// the other, so the walk over the tree recursed until the stack overflowed
// (#260). A cobra tree cannot hold a cycle, so the run stops and names it.
//
// A loop across levels, hook/kid registering hook, left hook's directory with
// no root, so both vanished from the manifest with no warning. A loop nothing
// registers stops the run too, since it cannot compile either.
func TestRegenerateManifest_StopsAtACycle(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		sources map[string]string
		loop    string
	}{
		"in one directory": {map[string]string{
			"hook": registeringSource("hook", "hook/kid", "ring"),
			"ring": registeringSource("ring", "loop"),
			"loop": registeringSource("loop", "ring"),
		}, "ring registers /work/pkg/cmd/loop registers /work/pkg/cmd/ring"},
		"past a finished sibling": {map[string]string{
			"hook": registeringSource("hook", "hook/kid", "ring"),
			"ring": registeringSource("ring", "zzz", "loop"),
			"zzz":  registeringSource("zzz"),
			"loop": registeringSource("loop", "ring"),
		}, "ring registers /work/pkg/cmd/loop registers /work/pkg/cmd/ring"},
		"across levels": {map[string]string{
			"hook/kid": registeringSource("hook/kid", "hook/kid/before", "hook"),
		}, "hook registers /work/pkg/cmd/hook/kid registers /work/pkg/cmd/hook"},
		"that nothing registers": {map[string]string{
			"ring": registeringSource("ring", "loop"),
			"loop": registeringSource("loop", "ring"),
		}, "loop registers /work/pkg/cmd/ring registers /work/pkg/cmd/loop"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, tc.sources)

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandCycle)
			require.ErrorContains(t, err, ": /work/pkg/cmd/"+tc.loop)

			raw, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, unreadableManifest, raw, "the manifest has not been changed")
		})
	}
}

// Following a refusal's hint has to leave a manifest that keeps the command:
// the scan finds a generated constructor only by its directory's name, so a
// hint that moves a package without renaming its constructor would lose it.
func TestRegenerateManifest_FollowingTheHintKeepsTheCommand(t *testing.T) {
	t.Parallel()

	withUse := func(dir, use string, children ...string) string {
		return strings.Replace(registeringSource(dir, children...), `Use: "`+path.Base(dir)+`"`, `Use: "`+use+`"`, 1)
	}

	for name, tc := range map[string]struct {
		sources map[string]string
		hint    string
		follow  func(t *testing.T, fs afero.Fs)
		want    []string
	}{
		"move a command named apart from its directory": {
			map[string]string{"hook/kid": withUse("hook/kid", "child")},
			"Move /work/pkg/cmd/hook/kid there with its constructor renamed from NewCmdKid to NewCmdChild",
			func(t *testing.T, fs afero.Fs) {
				t.Helper()
				moveCommand(t, fs, "hook", "hook/kid", "hook/child")
			},
			[]string{"hook", "hook child"},
		},
		"set its Use back instead": {
			map[string]string{"hook/kid": withUse("hook/kid", "child")},
			`Or set the name in its Use back to "kid"`,
			func(t *testing.T, fs afero.Fs) {
				t.Helper()
				require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/kid/cmd.go", []byte(registeringSource("hook/kid")), 0o644))
			},
			[]string{"hook", "hook kid"},
		},
		"move the occupant out first": {
			map[string]string{
				"hook":        registeringSource("hook", "hook/kid", "hook/status"),
				"hook/kid":    withUse("hook/kid", "status"),
				"hook/status": withUse("hook/status", "health"),
			},
			"Move /work/pkg/cmd/hook/status to /work/pkg/cmd/hook/health with its constructor renamed from NewCmdStatus to NewCmdHealth first, " +
				"then /work/pkg/cmd/hook/kid to /work/pkg/cmd/hook/status with its constructor renamed from NewCmdKid to NewCmdStatus",
			func(t *testing.T, fs afero.Fs) {
				t.Helper()
				moveCommand(t, fs, "hook", "hook/status", "hook/health")
				moveCommand(t, fs, "hook", "hook/kid", "hook/status")
			},
			[]string{"hook", "hook health", "hook status"},
		},
		"swap two commands in each other's places": {
			map[string]string{
				"hook":     registeringSource("hook", "hook/kid", "hook/one"),
				"hook/kid": withUse("hook/kid", "one"),
				"hook/one": withUse("hook/one", "kid"),
			},
			"Swap the two directories, so that /work/pkg/cmd/hook/", // completed below, as either may be met first

			func(t *testing.T, fs afero.Fs) {
				t.Helper()
				moveCommand(t, fs, "hook", "hook/kid", "hook/tmp")
				moveCommand(t, fs, "hook", "hook/one", "hook/kid")
				moveCommand(t, fs, "hook", "hook/tmp", "hook/one")
			},
			[]string{"hook", "hook kid", "hook one"},
		},
		"rename one of two commands of one name": {
			map[string]string{
				"hook":      registeringSource("hook", "hook/kid2", "hook/kid"),
				"hook/kid":  registeringSource("hook/kid"),
				"hook/kid2": withUse("hook/kid2", "kid"),
			},
			"Rename one of them, then move each package to the directory its name gives, name its constructor NewCmd<Directory>",
			func(t *testing.T, fs afero.Fs) {
				t.Helper()
				require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/kid2/cmd.go", []byte(withUse("hook/kid2", "eins")), 0o644))
				moveCommand(t, fs, "hook", "hook/kid2", "hook/eins")
			},
			[]string{"hook", "hook eins", "hook kid"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, tc.sources)

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandMisplaced)
			assert.Contains(t, errors.FlattenHints(err), tc.hint)

			if strings.HasPrefix(tc.hint, "Swap") {
				assert.Regexp(t, `moves to /work/pkg/cmd/hook/(one|kid) with its constructor renamed from NewCmd(Kid|One) to NewCmd(One|Kid) `+
					`and /work/pkg/cmd/hook/(one|kid) moves to /work/pkg/cmd/hook/(one|kid) with its constructor renamed`, errors.FlattenHints(err))
			}

			tc.follow(t, fs)
			require.NoError(t, g.RegenerateManifest(context.Background()))

			_, m := readRoundTripManifest(t, fs)
			assert.Equal(t, tc.want, commandPaths(m.Commands, ""))
		})
	}
}

// Moving a package without renaming its constructor leaves a registration the
// scan cannot follow, and the rebuild stops rather than drop the command.
func TestRegenerateManifest_RefusesARegistrationItCannotFollow(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, nil)
	moved := strings.Replace(registeringSource("hook", "hook/kid"), `"test-tool/pkg/cmd/hook/kid"`, `"test-tool/pkg/cmd/hook/child"`, 1)
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/cmd.go", []byte(moved), 0o644))
	require.NoError(t, fs.Rename("/work/pkg/cmd/hook/kid", "/work/pkg/cmd/hook/child"))
	before, _ := readRoundTripManifest(t, fs)

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandNotFound)
	require.ErrorContains(t, err, "/work/pkg/cmd/hook registers NewCmdKid from /work/pkg/cmd/hook/child")
	assert.Contains(t, errors.FlattenHints(err), "each file in /work/pkg/cmd/hook/child other than main.go, root.go and tests: NewCmdChild")

	after, _ := readRoundTripManifest(t, fs)
	assert.Equal(t, before, after, "nothing is written")
}

// The scan reads no command from root.go, so a constructor kept there is
// refused like a moved one, and the hint names the file rather than a rename
// that would change nothing.
func TestRegenerateManifest_RefusesAConstructorInRootGo(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, nil)
	require.NoError(t, fs.Rename("/work/pkg/cmd/hook/kid/cmd.go", "/work/pkg/cmd/hook/kid/root.go"))

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandNotFound)
	assert.Contains(t, errors.FlattenHints(err), "other than main.go, root.go and tests")
	assert.Contains(t, errors.FlattenHints(err), "Move it out of root.go")
}

// A package the scan read one command from can still hold a registered
// constructor it did not read, and that one would drop out of the manifest.
func TestRegenerateManifest_RefusesAConstructorBesideAnotherCommand(t *testing.T) {
	t.Parallel()

	const util = `package child
import "github.com/spf13/cobra"
func helper() *cobra.Command { return &cobra.Command{Use: "helper"} }`

	const other = `package kid
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
func NewCmdOther(props *props.Props) *setup.Command {
	return setup.Wrap("other", &cobra.Command{Use: "other"})
}`

	for name, tc := range map[string]struct {
		prepare func(t *testing.T, fs afero.Fs)
		fn, dir string
	}{
		"two constructors in one package": {func(t *testing.T, fs afero.Fs) {
			t.Helper()
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/kid/other.go", []byte(other), 0o644))
			src := strings.Replace(registeringSource("hook", "hook/kid"), "\treturn cmd", "\tcmd.AddCommand(ca.NewCmdOther(props))\n\treturn cmd", 1)
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/cmd.go", []byte(src), 0o644))
		}, "NewCmdOther", "/work/pkg/cmd/hook/kid"},
		"a move half followed beside a cobra helper": {func(t *testing.T, fs afero.Fs) {
			t.Helper()
			src := strings.Replace(registeringSource("hook", "hook/kid"), `"test-tool/pkg/cmd/hook/kid"`, `"test-tool/pkg/cmd/hook/child"`, 1)
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/cmd.go", []byte(src), 0o644))
			require.NoError(t, fs.Rename("/work/pkg/cmd/hook/kid", "/work/pkg/cmd/hook/child"))
			require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/hook/child/util.go", []byte(util), 0o644))
		}, "NewCmdKid", "/work/pkg/cmd/hook/child"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, nil)
			tc.prepare(t, fs)
			before, _ := readRoundTripManifest(t, fs)

			err := g.RegenerateManifest(context.Background())
			require.ErrorIs(t, err, ErrCommandNotFound)
			require.ErrorContains(t, err, "/work/pkg/cmd/hook registers "+tc.fn+" from "+tc.dir)
			assert.Contains(t, errors.FlattenHints(err), "Give "+tc.fn+" its own package at the directory its name gives")

			after, _ := readRoundTripManifest(t, fs)
			assert.Equal(t, before, after, "nothing is written")
		})
	}
}

// The package clause is the command's name as Go spells a package, so a
// hyphenated name is not written as one.
func TestNamesToFollow_SpellsThePackageAsGoDoes(t *testing.T) {
	t.Parallel()

	got := namesToFollow(&commandEntry{dir: "/work/pkg/cmd/hook/kid"}, "/work/pkg/cmd/hook/dry-run")
	assert.Contains(t, got, "the package clause to package dry_run, the options type to DryRunOptions, the run function to RunDryRun")
}

// A registration the scan cannot follow into a package it did read, or one of a
// function not named as a constructor, or one from outside pkg/cmd, is not a
// package a move left behind, so the rebuild goes ahead as it always has.
func TestRegenerateManifest_GoesAheadPastARegistrationNoMoveLeft(t *testing.T) {
	t.Parallel()

	const helper = `package hook
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ca "test-tool/pkg/cmd/hook/kid"
	REGISTRANT
)
func NewCmdHook(props *props.Props) *setup.Command {
	cmd := setup.Wrap("hook", &cobra.Command{Use: "hook", Short: "hook"})
	cmd.AddCommand(ca.NewCmdKid(props))
	cmd.AddCommand(CALL)
	return cmd
}
func newExtraCmd() *setup.Command {
	return setup.Wrap("extra", &cobra.Command{Use: "extra"})
}
func NewCmdBeside() *setup.Command {
	return setup.Wrap("beside", &cobra.Command{Use: "beside"})
}`

	for name, tc := range map[string]struct{ imp, call string }{
		"a helper beside its parent":    {"", "newExtraCmd()"},
		"a NewCmd beside its parent":    {"", "NewCmdBeside()"},
		"a wrapper around a command":    {`shared "test-tool/pkg/cmd/shared"`, "shared.Hidden(ca.NewCmdKid(props))"},
		"a constructor outside pkg/cmd": {`lib "test-tool/pkg/lib/extra"`, "lib.NewCmdExtra(props)"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs, g := subtreeProject(t, map[string]string{
				"hook": strings.NewReplacer("REGISTRANT", tc.imp, "CALL", tc.call).Replace(helper),
			})
			require.NoError(t, g.RegenerateManifest(context.Background()))

			_, m := readRoundTripManifest(t, fs)
			assert.Equal(t, []string{"hook", "hook kid"}, commandPaths(m.Commands, ""))
		})
	}
}

// The check compares registrations against the module path, so a go.mod it
// cannot read stops the rebuild rather than switch the check off.
func TestRegenerateManifest_RefusesWithoutAModulePath(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, nil)
	require.NoError(t, fs.Remove("/work/go.mod"))

	err := g.RegenerateManifest(context.Background())
	require.ErrorIs(t, err, ErrCommandSourceUnreadable)
	assert.Contains(t, errors.FlattenHints(err), "reads the module path from go.mod. Restore it")
}

// The scan reads no commands from internal, external or assets, so a
// registration from one of them is not a command it lost.
func TestRegenerateManifest_IgnoresARegistrationFromADirectoryItSkips(t *testing.T) {
	t.Parallel()

	for _, dir := range []string{"internal/extra", "external", "hook/assets"} {
		fs, g := subtreeProject(t, map[string]string{"hook": registeringSource("hook", "hook/kid", dir)})
		require.NoError(t, g.RegenerateManifest(context.Background()), dir)

		_, m := readRoundTripManifest(t, fs)
		assert.Equal(t, []string{"hook", "hook kid"}, commandPaths(m.Commands, ""), dir)
	}
}

// A constructor registered twice by one command is one command, so the
// manifest lists it once.
func TestRegenerateManifest_RecordsARepeatedRegistrationOnce(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{"hook": registeringSource("hook", "hook/kid", "hook/kid")})
	require.NoError(t, g.RegenerateManifest(context.Background()))

	_, m := readRoundTripManifest(t, fs)
	assert.Equal(t, []string{"hook", "hook kid"}, commandPaths(m.Commands, ""))
}

// moveCommand moves the package at pkg/cmd/from to pkg/cmd/to, renames its
// constructor after the new directory, and repoints the registrant at it.
func moveCommand(t *testing.T, fs afero.Fs, registrant, from, to string) {
	t.Helper()

	oldFn, newFn := "NewCmd"+PascalCase(path.Base(from)), "NewCmd"+PascalCase(path.Base(to))

	src, err := afero.ReadFile(fs, "/work/pkg/cmd/"+from+"/cmd.go")
	require.NoError(t, err)
	require.NoError(t, fs.RemoveAll("/work/pkg/cmd/"+from))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/"+to+"/cmd.go",
		[]byte(strings.Replace(string(src), "func "+oldFn+"(", "func "+newFn+"(", 1)), 0o644))

	reg, err := afero.ReadFile(fs, "/work/pkg/cmd/"+registrant+"/cmd.go")
	require.NoError(t, err)

	alias := regexp.MustCompile(`(\w+) "test-tool/pkg/cmd/` + regexp.QuoteMeta(from) + `"`).FindStringSubmatch(string(reg))
	require.NotNil(t, alias, "the registrant imports %s", from)

	out := strings.Replace(string(reg), `"test-tool/pkg/cmd/`+from+`"`, `"test-tool/pkg/cmd/`+to+`"`, 1)
	out = strings.Replace(out, alias[1]+"."+oldFn+"(", alias[1]+"."+newFn+"(", 1)
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/"+registrant+"/cmd.go", []byte(out), 0o644))
}

func commandPaths(cmds []ManifestCommand, prefix string) []string {
	var out []string

	for _, c := range cmds {
		p := strings.TrimSpace(prefix + " " + c.Name)
		out = append(out, p)
		out = append(out, commandPaths(c.Commands, p)...)
	}

	return out
}
