package setup

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func settingByKey(t *testing.T, settings []SourceSetting, key string) SourceSetting {
	t.Helper()

	for _, s := range settings {
		if s.Key == key {
			return s
		}
	}

	require.Failf(t, "setting not declared", "%q", key)

	return SourceSetting{}
}

// One catalogue of what each shipped kind's init config asks, so the
// generator's wizard can say what a slot's settings are without linking the
// kinds and their SDKs.
func TestConfigSourceSettings(t *testing.T) {
	t.Parallel()

	path := settingByKey(t, ConfigSourceSettings("file", "mytool", "extra"), "path")
	assert.True(t, path.Required)
	assert.NotEmpty(t, path.Description)

	service := settingByKey(t, ConfigSourceSettings("keychain", "mytool", "tokens"), "service")
	assert.Equal(t, "mytool", service.Default, "the keychain service defaults to the tool's name")
	assert.Contains(t, service.Description, "config.sources.tokens.keys")

	assert.Equal(t, "secret", settingByKey(t, ConfigSourceSettings("vault", "mytool", "v"), "mount").Default)

	s3 := ConfigSourceSettings("aws-s3", "mytool", "s")
	settingByKey(t, s3, "bucket")
	settingByKey(t, s3, "region")

	settingByKey(t, ConfigSourceSettings("azure-blob", "mytool", "b"), "tenant_id")
	settingByKey(t, ConfigSourceSettings("gcp-secret", "mytool", "g"), "project")

	assert.Nil(t, ConfigSourceSettings("etcd", "mytool", "e"), "an override-only kind's code reads its own settings")
	assert.Nil(t, ConfigSourceSettings("zookeeper", "mytool", "z"))
}

func TestConfigSourceInitialiserFor_NamesTheSlot(t *testing.T) {
	t.Parallel()

	init := ConfigSourceInitialiserFor("file")(nil, props.ConfigSource{Name: "extra", Kind: "file"})
	assert.Equal(t, "extra", init.Name())
}

type reportingBootstrap struct {
	ConfigBootstrap
	got string
}

func (r *reportingBootstrap) ReportCredential(origin string) { r.got = origin }

// A factory says which rung answered; a bootstrap that does not listen is
// left alone, so a test's fake bootstrap needs nothing new.
func TestReportSourceCredential(t *testing.T) {
	t.Parallel()

	r := &reportingBootstrap{}
	ReportSourceCredential(r, "auth.keychain")
	assert.Equal(t, "auth.keychain", r.got)

	assert.NotPanics(t, func() { ReportSourceCredential(struct{ ConfigBootstrap }{}, "auth.env") })
}

type closingBootstrap struct {
	ConfigBootstrap
	got []io.Closer
}

func (c *closingBootstrap) CloseWithStore(closer io.Closer) { c.got = append(c.got, closer) }

// A factory hands the store what it built and must be closed; a bootstrap
// that does not listen is left alone.
func TestCloseWithStore(t *testing.T) {
	t.Parallel()

	c := &closingBootstrap{}
	closer := io.NopCloser(nil)
	CloseWithStore(c, closer)
	assert.Equal(t, []io.Closer{closer}, c.got)

	assert.NotPanics(t, func() { CloseWithStore(struct{ ConfigBootstrap }{}, closer) })
}
