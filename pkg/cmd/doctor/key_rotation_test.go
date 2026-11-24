package doctor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/signing/minisign"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestCheckKeyRotation pins spec 0101 D18: doctor warns while a tool with
// rotation authority keys has update.rotation.enabled set false.
func TestCheckKeyRotation(t *testing.T) {
	t.Parallel()

	testutil.EachConfigDir(t, testCheckKeyRotation)
}

func testCheckKeyRotation(t *testing.T, style p.ConfigDir) {
	t.Parallel()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	authority, err := minisign.NewPublicKey(pub)
	require.NoError(t, err)

	withAuthority := func(yaml string) *p.Props {
		return &p.Props{
			Config: testutil.StoreFromYAML(t, yaml),
			Tool: p.Tool{Name: "t", Config: p.ConfigSpec{Dir: style}, Signing: p.SigningConfig{
				EmbeddedKeys:          [][]byte{[]byte("release key")},
				RotationAuthorityKeys: []string{authority.Encode()},
			}},
		}
	}

	t.Run("a tool without authority keys is skipped", func(t *testing.T) {
		t.Parallel()

		res := checkKeyRotation(context.Background(), &p.Props{Tool: p.Tool{Name: "t"}})
		assert.Equal(t, CheckSkip, res.Status)
	})

	t.Run("update disabled is skipped", func(t *testing.T) {
		t.Parallel()

		props := withAuthority("{}\n")
		props.Tool.Features = p.SetFeatures(p.Disable(p.UpdateCmd))
		assert.Equal(t, CheckSkip, checkKeyRotation(context.Background(), props).Status)
	})

	t.Run("on by default passes", func(t *testing.T) {
		t.Parallel()

		res := checkKeyRotation(context.Background(), withAuthority("{}\n"))
		assert.Equal(t, CheckPass, res.Status, res.Message)
	})

	t.Run("a malformed authority key fails", func(t *testing.T) {
		t.Parallel()

		props := withAuthority("{}\n")
		props.Tool.Signing.RotationAuthorityKeys = []string{"RWkey"}
		assert.Equal(t, CheckFail, checkKeyRotation(context.Background(), props).Status)
	})

	t.Run("authority keys with no release key warn", func(t *testing.T) {
		t.Parallel()

		props := withAuthority("{}\n")
		props.Tool.Signing.EmbeddedKeys = nil
		res := checkKeyRotation(context.Background(), props)
		require.Equal(t, CheckWarn, res.Status, res.Message)
		assert.Contains(t, res.Message, "nothing to rotate")
	})

	t.Run("turned off warns and names the key", func(t *testing.T) {
		t.Parallel()

		res := checkKeyRotation(context.Background(), withAuthority("update:\n  rotation:\n    enabled: false\n"))
		require.Equal(t, CheckWarn, res.Status, res.Message)
		assert.Contains(t, res.Message, setup.ConfigKeyUpdateRotationEnabled)
	})

	// doctor loads the record update loads, so a corrupt one fails here
	// rather than passing while update refuses (go-tool-base#205).
	t.Run("a corrupt rotation record fails, as update would", func(t *testing.T) {
		t.Parallel()

		for name, record := range map[string]string{
			"not JSON":                  "{",
			"a key that does not parse": `{"epoch":1,"added":["not a key"]}`,
		} {
			props := withAuthority("{}\n")
			props.FS = afero.NewMemMapFs()
			dir := setup.ConfigDir(props.Tool)
			require.NotEmpty(t, dir)
			require.NoError(t, afero.WriteFile(props.FS, filepath.Join(dir, "rotation.json"), []byte(record), 0o600))

			res := checkKeyRotation(context.Background(), props)
			require.Equal(t, CheckFail, res.Status, name)
			assert.Contains(t, res.Message, "update refuses to run", name)
			assert.Contains(t, res.Message, "rotation.json", name)
			assert.Contains(t, res.Details, "ask the tool's maintainers first", name)
		}
	})

	t.Run("a config directory in a git checkout warns that a revoke is forgotten", func(t *testing.T) {
		t.Parallel()

		props := withAuthority("{}\n")
		props.FS = afero.NewMemMapFs()
		dir := setup.ConfigDir(props.Tool)
		require.NoError(t, props.FS.MkdirAll(filepath.Join(filepath.Dir(dir), ".git"), 0o700))

		res := checkKeyRotation(context.Background(), props)
		require.Equal(t, CheckWarn, res.Status, res.Message)
		assert.Contains(t, res.Message, "trusted again")
		assert.Contains(t, res.Message, dir+" is inside a git checkout")
		assert.Contains(t, res.Details, "outside any git checkout")
	})

	t.Run("no rotation record yet passes", func(t *testing.T) {
		t.Parallel()

		props := withAuthority("{}\n")
		props.FS = afero.NewMemMapFs()
		res := checkKeyRotation(context.Background(), props)
		assert.Equal(t, CheckPass, res.Status, res.Message)
	})

	t.Run("it is in the default check set", func(t *testing.T) {
		t.Parallel()

		want := reflect.ValueOf(checkKeyRotation).Pointer()
		found := false

		for _, check := range DefaultChecks(&p.Props{}) {
			if reflect.ValueOf(check).Pointer() == want {
				found = true
			}
		}

		assert.True(t, found, "checkKeyRotation must be a default check")
	})
}
