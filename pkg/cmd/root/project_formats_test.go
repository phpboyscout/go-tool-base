package root

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configdotenv "gitlab.com/phpboyscout/go/config-dotenv"
	confighcl "gitlab.com/phpboyscout/go/config-hcl"
	configini "gitlab.com/phpboyscout/go/config-ini"
	configjson "gitlab.com/phpboyscout/go/config-json"
	configproperties "gitlab.com/phpboyscout/go/config-properties"
	configtoml "gitlab.com/phpboyscout/go/config-toml"
	configxml "gitlab.com/phpboyscout/go/config-xml"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// allFormats links every format of the family on a fresh registry.
var allFormats = []setup.ConfigCodec{
	{Format: "toml", Codec: configtoml.Codec{}, Extensions: []string{".toml"}},
	{Format: "json", Codec: configjson.Codec{}, Extensions: []string{".json"}},
	{Format: "hcl", Codec: confighcl.Codec{}, Extensions: []string{".hcl"}},
	{Format: "ini", Codec: configini.Codec{}, Extensions: []string{".ini"}},
	{Format: "xml", Codec: configxml.Codec{}, Extensions: []string{".xml"}},
	{Format: "dotenv", Codec: configdotenv.Codec{}, Extensions: []string{".env"}},
	{Format: "properties", Codec: configproperties.Codec{}, Extensions: []string{".properties"}},
}

func allFormatsProps(t *testing.T, fs afero.Fs, log logger.Logger) *p.Props {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	for _, c := range allFormats {
		require.NoError(t, reg.Declare(setup.ConfigFormatDescriptor(c.Format)))
		setup.RegisterConfigCodecOn(reg, c.Format, c.Codec, c.Extensions...)
	}

	props, err := p.New(p.Tool{Name: "mytool"}, log, fs, p.WithFeatures(reg.Snapshot()))
	require.NoError(t, err)

	return props
}

// hostileByFormat is hostileProjectConfig in each format, as far as the
// format can say it: a .env file cannot spell require_signature, since its
// underscores are path separators. Flat dotted spellings are included where a
// format keeps them flat, so a core that began reading them as paths would
// fail here rather than quietly let them through.
var hostileByFormat = map[string]struct {
	content   string
	protected []string
}{
	".yaml": {
		content:   hostileProjectConfig + "\"update.require_checksum\": false\n",
		protected: []string{"update.require_signature", "update.require_checksum", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".toml": {
		content:   "[update]\nrequire_signature = false\nrequire_checksum = false\npolicy = \"disabled\"\n[telemetry]\nenabled = true\n[github.auth]\nvalue = \"ghp_hostile\"\n[log]\nlevel = \"debug\"\n",
		protected: []string{"update.require_signature", "update.require_checksum", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".json": {
		content:   `{"update":{"require_signature":false,"policy":"disabled"},"update.require_checksum":false,"telemetry":{"enabled":true},"github":{"auth":{"value":"ghp_hostile"}},"log":{"level":"debug"}}`,
		protected: []string{"update.require_signature", "update.require_checksum", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".hcl": {
		content:   "update {\n  require_signature = false\n  policy = \"disabled\"\n}\ntelemetry {\n  enabled = true\n}\ngithub {\n  auth {\n    value = \"ghp_hostile\"\n  }\n}\nlog {\n  level = \"debug\"\n}\n",
		protected: []string{"update.require_signature", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".ini": {
		content:   "[update]\nrequire_signature = false\npolicy = disabled\n[telemetry]\nenabled = true\n[github.auth]\nvalue = ghp_hostile\n[log]\nlevel = debug\n",
		protected: []string{"update.require_signature", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".xml": {
		content:   "<config><update><require_signature>false</require_signature><policy>disabled</policy></update><telemetry><enabled>true</enabled></telemetry><github><auth><value>ghp_hostile</value></auth></github><log><level>debug</level></log></config>",
		protected: []string{"update.require_signature", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".env": {
		content:   "UPDATE_POLICY=disabled\nTELEMETRY_ENABLED=true\nGITHUB_AUTH_VALUE=ghp_hostile\nLOG_LEVEL=debug\n",
		protected: []string{"update.policy", "telemetry.enabled", "github.auth.value"},
	},
	".properties": {
		content:   "update.require_signature=false\nupdate.policy=disabled\ntelemetry.enabled=true\ngithub.auth.value=ghp_hostile\nlog.level=debug\n",
		protected: []string{"update.require_signature", "update.policy", "telemetry.enabled", "github.auth.value"},
	},
}

// Spec 0204 D17: the trust filter wraps whichever codec reads the project
// file, so an untrusted .mytool.toml has exactly what an untrusted
// .mytool.yaml has stripped. The table is the headline hostile-clone test run
// once per format, asserted on the resolved view rather than on the decoded
// map, because what matters is what the tool ends up reading.
func TestProjectLocalTrust_HostileCloneIsIgnoredInEveryFormat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()

	for ext, hostile := range hostileByFormat {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".mytool"+ext)
			require.NoError(t, afero.WriteFile(fs, path, []byte(hostile.content), 0o600))

			log := logger.NewBuffer()
			store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
				Props:             allFormatsProps(t, fs, log),
				AllowEmpty:        true,
				ProjectConfigPath: path,
			})
			require.NoError(t, err)

			view := store.View()
			for _, key := range hostile.protected {
				assert.Falsef(t, view.IsSet(key), "an untrusted %s project file must not set %s", ext, key)
			}

			assert.Equal(t, "debug", view.GetString("log.level"), "a workflow key still applies")
			assert.True(t, log.Contains("ignoring security-sensitive keys"), "the ignored keys are logged")
		})
	}
}

// Trusted, a project file in a writable format is read in full and written
// through its own codec.
func TestProjectLocalTrust_TrustedTOMLIsReadAndWritable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	path := filepath.Join(t.TempDir(), ".mytool.toml")
	require.NoError(t, afero.WriteFile(fs, path, []byte(hostileByFormat[".toml"].content), 0o600))
	require.NoError(t, setup.TrustProjectConfig(fs, "mytool", path))

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:             allFormatsProps(t, fs, logger.NewNoop()),
		AllowEmpty:        true,
		ProjectConfigPath: path,
	})
	require.NoError(t, err)
	assert.Equal(t, "ghp_hostile", store.View().GetString("github.auth.value"))

	_, err = store.Apply(t.Context(), config.Set("log.level", "warn"))
	require.NoError(t, err)

	written, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Contains(t, string(written), `level = "warn"`)
}

// Discovery refusing two candidates stops the command before any store is
// built (spec 0204 D16).
func TestProjectConfigLayer_RefusesTwoCandidates(t *testing.T) {
	// Not parallel: t.Chdir is process-wide.
	dir := t.TempDir()
	require.NoError(t, afero.WriteFile(afero.NewOsFs(), filepath.Join(dir, ".mytool.yaml"), []byte("a: 1\n"), 0o600))
	require.NoError(t, afero.WriteFile(afero.NewOsFs(), filepath.Join(dir, ".mytool.toml"), []byte("a = 1\n"), 0o600))
	t.Chdir(dir)

	_, err := projectConfigLayer(allFormatsProps(t, afero.NewOsFs(), logger.NewNoop()), newConfigFlagCmd(t))
	require.ErrorIs(t, err, setup.ErrAmbiguousProjectConfig)
	assert.NotEmpty(t, errors.FlattenHints(err))
}
