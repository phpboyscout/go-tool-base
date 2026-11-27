package generator

import (
	"context"
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

// A command directly in pkg/cmd meets every other package in the top-level
// scan, so the manifest records its registration from another subtree. That
// regenerate project then expects the package elsewhere is #262.
func TestRegenerateManifest_PlacesATopLevelRegistrationFromAnotherSubtree(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{
		"hook":     registeringSource("hook", "hook/kid", "lib/one"),
		"hook/kid": registeringSource("hook/kid", "hook/kid/before"),
	})

	require.NoError(t, g.RegenerateManifest(context.Background()))

	_, m := readRoundTripManifest(t, fs)
	require.Len(t, m.Commands, 1)
	assert.Equal(t, []string{"kid", "one"}, commandNames(m.Commands[0].Commands))
}

// A directory's scan links every command it holds, so the manifest records a
// registration of any package under the directory that holds the registrant, a
// sibling's included. That regenerate project then expects it elsewhere is #262.
func TestRegenerateManifest_PlacesARegistrationUnderTheDirectoryHoldingIt(t *testing.T) {
	t.Parallel()

	fs, g := subtreeProject(t, map[string]string{
		"hook/kid":      registeringSource("hook/kid", "hook/kid/before", "hook/side/one"),
		"hook/side/one": registeringSource("hook/side/one"),
	})

	require.NoError(t, g.RegenerateManifest(context.Background()))

	_, m := readRoundTripManifest(t, fs)
	require.Len(t, m.Commands, 1)
	require.Len(t, m.Commands[0].Commands, 1)
	assert.Equal(t, []string{"before", "one"}, commandNames(m.Commands[0].Commands[0].Commands))
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

func commandNames(cmds []ManifestCommand) []string {
	names := make([]string, 0, len(cmds))
	for _, c := range cmds {
		names = append(names, c.Name)
	}

	return names
}
