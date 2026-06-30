package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestManifestSigning_RoundTrip proves the signing block survives a
// marshal/unmarshal cycle, so `gtb regenerate` reproduces the chosen
// signing posture deterministically.
func TestManifestSigning_RoundTrip(t *testing.T) {
	t.Parallel()

	m := Manifest{}
	m.Properties.Signing = ManifestSigning{
		Enabled:                   true,
		ExternalKeyEmail:          "release@example.test",
		RequireSignature:          true,
		KeySource:                 "both",
		RequireExternalCrosscheck: true,
	}

	data, err := yaml.Marshal(m)
	require.NoError(t, err)
	assert.Contains(t, string(data), "external_key_email: release@example.test")
	assert.Contains(t, string(data), "require_signature: true")

	var got Manifest
	require.NoError(t, yaml.Unmarshal(data, &got))
	assert.Equal(t, m.Properties.Signing, got.Properties.Signing)
}

// newSkeletonGeneratorForTest builds a generator whose runCommand is a
// no-op, so GenerateSkeleton writes files without shelling out to
// `go mod tidy` / golangci-lint.
func newSkeletonGeneratorForTest(t *testing.T, fs afero.Fs) *Generator {
	t.Helper()

	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: config.NewFilesContainer(fs, config.WithLogger(l)),
	}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	return g
}

func signingSkeletonConfig(path string, signing ManifestSigning) SkeletonConfig {
	return SkeletonConfig{
		Name:        "sign-tool",
		Repo:        "test/sign-tool",
		Host:        "github.com",
		Description: "signing scaffold test",
		Path:        path,
		// changelog/docs off so the skeleton needs no external tools.
		Features: []ManifestFeature{
			{Name: "changelog", Enabled: false},
			{Name: "docs", Enabled: false},
		},
		Signing: signing,
	}
}

// TestGenerateSkeleton_SigningEnabled asserts that an enabled signing
// posture scaffolds the trustkeys package, wires props.Signing into the
// root command, and emits the enforcement defaults.
func TestGenerateSkeleton_SigningEnabled(t *testing.T) {
	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	cfg := signingSkeletonConfig(path, ManifestSigning{
		Enabled:          true,
		ExternalKeyEmail: "release@example.test",
	})
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	assert.FileExists(t, filepath.Join(path, "internal", "trustkeys", "trustkeys.go"))
	assert.FileExists(t, filepath.Join(path, "internal", "trustkeys", "keys", ".gitkeep"))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "Signing: props.SigningConfig{")

	signingGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "signing.go"))
	require.NoError(t, err)
	assert.Contains(t, string(signingGo), `verify.DefaultExternalKeyEmail = "release@example.test"`)
}

// TestGenerateSkeleton_SigningDisabled asserts the default (disabled)
// posture scaffolds nothing signing-related.
func TestGenerateSkeleton_SigningDisabled(t *testing.T) {
	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	require.NoError(t, g.GenerateSkeleton(context.Background(), signingSkeletonConfig(path, ManifestSigning{})))

	assert.NoFileExists(t, filepath.Join(path, "internal", "trustkeys", "trustkeys.go"))
	assert.NoFileExists(t, filepath.Join(path, "pkg", "cmd", "root", "signing.go"))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(cmdGo), "Signing:")
}
