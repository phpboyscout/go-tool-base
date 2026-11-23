package setup

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/signing/verify"

	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0221 D1.
func TestConfigDirUnder(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(filepath.Separator), "home", "ada")
	xdg := filepath.Join(string(filepath.Separator), "xdg")

	abs := func(path string) string {
		p, err := filepath.Abs(path)
		require.NoError(t, err)

		return p
	}

	for name, tc := range map[string]struct {
		style      props.ConfigDir
		home, xdg  string
		wantResult string
	}{
		"empty style is home":              {"", home, "", filepath.Join(home, ".mytool")},
		"home":                             {props.ConfigDirHome, home, "", filepath.Join(home, ".mytool")},
		"home ignores XDG_CONFIG_HOME":     {props.ConfigDirHome, home, abs(xdg), filepath.Join(home, ".mytool")},
		"home without a home directory":    {props.ConfigDirHome, "", abs(xdg), ""},
		"xdg, variable unset or empty":     {props.ConfigDirXDG, home, "", filepath.Join(home, ".config", "mytool")},
		"xdg, variable relative":           {props.ConfigDirXDG, home, "conf", filepath.Join(home, ".config", "mytool")},
		"xdg, variable absolute":           {props.ConfigDirXDG, home, abs(xdg), filepath.Join(abs(xdg), "mytool")},
		"xdg, absolute, no home directory": {props.ConfigDirXDG, "", abs(xdg), filepath.Join(abs(xdg), "mytool")},
		"xdg, unset, no home directory":    {props.ConfigDirXDG, "", "", ""},
		"xdg, relative, no home directory": {props.ConfigDirXDG, "", "conf", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.wantResult, configDirUnder(tc.style, tc.home, tc.xdg, "MyTool"))
		})
	}
}

// Spec 0221 D4: everything kept under the user directory follows the style.
func TestUserDirectoryFollowsTheStyle(t *testing.T) {
	t.Parallel()

	testutil.EachConfigDir(t, testUserDirectoryFollowsTheStyle)
}

func testUserDirectoryFollowsTheStyle(t *testing.T, style props.ConfigDir) {
	t.Parallel()

	tool := props.Tool{Name: "styled-tool", Config: props.ConfigSpec{Dir: style}}
	dir := ConfigDir(tool)
	require.NotEmpty(t, dir)

	other := props.ConfigDirHome
	if style == other {
		other = props.ConfigDirXDG
	}

	otherDir := ConfigDir(props.Tool{Name: "styled-tool", Config: props.ConfigSpec{Dir: other}})
	require.NotEqual(t, otherDir, dir)

	fs := afero.NewMemMapFs()
	project := filepath.Join(string(filepath.Separator), "work", ".styled-tool.yaml")
	require.NoError(t, afero.WriteFile(fs, project, []byte("log:\n  level: debug\n"), 0o600))

	require.NoError(t, SetCheckedVersion(fs, tool, "v1.2.3"))
	require.NoError(t, SetTimeSinceLast(fs, tool, UpdatedKey))
	require.NoError(t, SetPinnedVersion(fs, tool, "v1.2.3"))
	require.NoError(t, TrustProjectConfig(fs, tool, project))

	for _, file := range []string{"last_checked", "last_updated", "pinned_version", trustStoreFilename} {
		exists, err := afero.Exists(fs, filepath.Join(dir, file))
		require.NoError(t, err)
		assert.True(t, exists, file)
	}

	assert.Equal(t, "v1.2.3", GetCheckedVersion(fs, tool))

	_, recorded := TimeSinceLast(fs, tool, UpdatedKey)
	assert.True(t, recorded)

	pinned, err := GetPinnedVersion(fs, tool)
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3", pinned)

	// The second record is added to the first, so it is read back first.
	second := filepath.Join(string(filepath.Separator), "other", ".styled-tool.yaml")
	require.NoError(t, afero.WriteFile(fs, second, []byte("log:\n  level: debug\n"), 0o600))
	require.NoError(t, TrustProjectConfig(fs, tool, second))

	trusted, err := ListTrustedProjects(fs, tool)
	require.NoError(t, err)
	assert.Len(t, trusted, 2)

	require.NoError(t, UntrustProjectConfig(fs, tool, second))

	isTrusted, err := IsProjectConfigTrusted(fs, tool, second)
	require.NoError(t, err)
	assert.False(t, isTrusted)

	isTrusted, err = IsProjectConfigTrusted(fs, tool, project)
	require.NoError(t, err)
	assert.True(t, isTrusted)

	assert.Less(t, GetTimeSinceLast(fs, tool, UpdatedKey), time.Minute)

	ensured, err := EnsureDefaultConfigDir(fs, tool)
	require.NoError(t, err)
	assert.Equal(t, dir, ensured)

	paths := DefaultConfigPaths(&props.Props{Tool: tool, FS: fs})
	assert.Equal(t, filepath.Join(dir, "config.yaml"), paths[len(paths)-1])

	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, rotationStoreFilename), []byte("not json"), 0o600))
	require.ErrorIs(t, CheckRotationRecord(context.Background(), fs, tool), ErrRotationStoreCorrupt)

	inCheckout := afero.NewMemMapFs()
	require.NoError(t, inCheckout.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	require.ErrorIs(t, CanPin(inCheckout, tool), ErrPinInCheckout)

	exists, err := afero.DirExists(fs, otherDir)
	require.NoError(t, err)
	assert.False(t, exists, "nothing is written to %s", otherDir)
}

// A record kept only in memory, or in the home directory, would forget a
// revoked key.
func TestConfigureRotation_KeepsTheRecordInTheToolsDirectory(t *testing.T) {
	t.Parallel()

	testutil.EachConfigDir(t, testConfigureRotation_KeepsTheRecordInTheToolsDirectory)
}

func testConfigureRotation_KeepsTheRecordInTheToolsDirectory(t *testing.T, style props.ConfigDir) {
	t.Parallel()

	tool := props.Tool{Name: "styled-tool", Config: props.ConfigSpec{Dir: style}}
	fs := afero.NewMemMapFs()
	p := injectProps(t, tool)
	p.FS = fs

	s, err := NewUpdater(t.Context(), p, "", false, withReleaseProvider(forgetest.New()))
	require.NoError(t, err)

	s.rotationEnabled, s.rotationAuthorityKeys = true, []string{"authority"}

	var cfg verify.KeyResolverConfig
	s.configureRotation(&cfg)
	require.NotNil(t, cfg.RotationStore)
	require.NoError(t, cfg.RotationStore.Save(t.Context(), verify.RotationRecord{Epoch: 7}))

	exists, err := afero.Exists(fs, filepath.Join(ConfigDir(tool), rotationStoreFilename))
	require.NoError(t, err)
	assert.True(t, exists)

	got, err := newRotationStore(fs, ConfigDir(tool)).Load(t.Context())
	require.NoError(t, err)
	assert.Equal(t, uint64(7), got.Epoch)
}
