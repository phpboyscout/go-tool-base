package telemetry

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configafero "gitlab.com/phpboyscout/go/config-afero"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestChecksRegistered(t *testing.T) {
	t.Parallel()

	providers := setup.GetChecks()[props.TelemetryCmd]
	require.NotEmpty(t, providers, "telemetry checks must be registered")

	var checks []setup.CheckFunc
	for _, provider := range providers {
		checks = append(checks, provider(&props.Props{})...)
	}

	assert.Len(t, checks, 2, "status + connectivity checks")
}

// fakeRequestor is a no-network telemetry.DeletionRequestor double.
type fakeRequestor struct{ err error }

func (f fakeRequestor) RequestDeletion(_ context.Context, _ string) error { return f.err }

func cfgWith(t *testing.T, yaml string) *config.Store {
	t.Helper()

	return testutil.StoreFromYAML(t, yaml)
}

// writableProps builds Props whose store routes writes to the tool's default
// config path on a MemMapFs, so setTelemetryEnabled has a file layer to land
// in — a missing file is a target the file backend creates.
func writableProps(t *testing.T, log logger.Logger) (*props.Props, afero.Fs) {
	t.Helper()

	memfs := afero.NewMemMapFs()
	dir := setup.GetDefaultConfigDir(memfs, "tool")
	require.NotEmpty(t, dir)

	target := filepath.Join(dir, setup.DefaultConfigFilename)

	store, err := config.NewStore(t.Context(), config.WithFiles(configafero.Wrap(memfs), target))
	require.NoError(t, err)

	return &props.Props{
		Tool:   props.Tool{Name: "tool"},
		Logger: log,
		Config: store,
		FS:     memfs,
	}, memfs
}

func TestResolveEndpoint(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://otel",
		resolveEndpoint(&props.Props{Tool: props.Tool{Telemetry: props.TelemetryConfig{OTelEndpoint: "https://otel", Endpoint: "https://ep"}}}),
		"OTelEndpoint takes precedence")
	assert.Equal(t, "https://ep",
		resolveEndpoint(&props.Props{Tool: props.Tool{Telemetry: props.TelemetryConfig{Endpoint: "https://ep"}}}))
	assert.Empty(t, resolveEndpoint(&props.Props{}))
}

func TestCheckTelemetryStatus(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		res := checkTelemetryStatus(context.Background(), &props.Props{Config: cfgWith(t, "{}\n")})
		assert.Equal(t, "skip", res.Status)
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{
			Config:    cfgWith(t, "telemetry:\n  enabled: true\n"),
			Collector: props.NoopCollector{},
		}
		res := checkTelemetryStatus(context.Background(), p)
		assert.Equal(t, "pass", res.Status)
		assert.Contains(t, res.Message, "enabled")
	})
}

func TestCheckTelemetryConnectivity(t *testing.T) {
	t.Parallel()

	enabled := func(endpoint string) *props.Props {
		return &props.Props{
			Config: cfgWith(t, "telemetry:\n  enabled: true\n"),
			Tool:   props.Tool{Telemetry: props.TelemetryConfig{Endpoint: endpoint}},
		}
	}

	t.Run("disabled skips", func(t *testing.T) {
		t.Parallel()

		res := checkTelemetryConnectivity(context.Background(), &props.Props{Config: cfgWith(t, "{}\n")})
		assert.Equal(t, "skip", res.Status)
	})

	t.Run("no endpoint skips", func(t *testing.T) {
		t.Parallel()

		res := checkTelemetryConnectivity(context.Background(), enabled(""))
		assert.Equal(t, "skip", res.Status)
	})

	t.Run("invalid URL fails", func(t *testing.T) {
		t.Parallel()

		res := checkTelemetryConnectivity(context.Background(), enabled("http://%zz"))
		assert.Equal(t, "fail", res.Status)
	})

	t.Run("reachable passes", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		res := checkTelemetryConnectivity(context.Background(), enabled(srv.URL))
		assert.Equal(t, "pass", res.Status)
	})

	t.Run("unreachable warns", func(t *testing.T) {
		t.Parallel()

		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		url := srv.URL
		srv.Close() // now refuses connections

		res := checkTelemetryConnectivity(context.Background(), enabled(url))
		assert.Equal(t, "warn", res.Status)
	})
}

func TestSetTelemetryEnabled_CreatesConfigWhenMissing(t *testing.T) {
	t.Parallel()

	p, memfs := writableProps(t, logger.NewNoop())

	// No config file exists yet — Apply routes to the declared file layer and
	// the file backend creates it.
	var out bytes.Buffer

	require.NoError(t, setTelemetryEnabled(t.Context(), p, true, &out))
	assert.Contains(t, out.String(), "Telemetry enabled")

	dir := setup.GetDefaultConfigDir(memfs, "tool")
	exists, _ := afero.Exists(memfs, filepath.Join(dir, setup.DefaultConfigFilename))
	assert.True(t, exists)
}

func TestBuildDeletionRequestor(t *testing.T) {
	t.Parallel()

	t.Run("custom requestor is used", func(t *testing.T) {
		t.Parallel()

		want := fakeRequestor{}
		p := &props.Props{Tool: props.Tool{Telemetry: props.TelemetryConfig{
			DeletionRequestor: func(*props.Props) any { return want },
		}}}
		assert.Equal(t, want, buildDeletionRequestor(p))
	})

	t.Run("custom returning a non-requestor falls back", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Logger: logger.NewBuffer(), Tool: props.Tool{Telemetry: props.TelemetryConfig{
			DeletionRequestor: func(*props.Props) any { return 42 },
		}}}
		assert.NotNil(t, buildDeletionRequestor(p))
	})

	t.Run("endpoint configured yields an HTTP-backed requestor", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Logger: logger.NewBuffer(), Tool: props.Tool{Telemetry: props.TelemetryConfig{Endpoint: "https://x"}}}
		assert.NotNil(t, buildDeletionRequestor(p))
	})

	t.Run("no endpoint yields a noop-backed requestor", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{Logger: logger.NewBuffer(), Tool: props.Tool{}}
		assert.NotNil(t, buildDeletionRequestor(p))
	})
}

func TestNewResetCmd(t *testing.T) {
	t.Parallel()

	makeResetProps := func(log logger.Logger, reqErr error, forceEnabled bool) *props.Props {
		p, _ := writableProps(t, log)
		p.Tool.Telemetry = props.TelemetryConfig{
			ForceEnabled:      forceEnabled,
			DeletionRequestor: func(*props.Props) any { return fakeRequestor{err: reqErr} },
		}
		p.Collector = props.NoopCollector{}

		return p
	}

	t.Run("clears data, requests deletion, disables", func(t *testing.T) {
		t.Parallel()

		cmd := newResetCmd(makeResetProps(logger.NewNoop(), nil, false))
		cmd.SetContext(context.Background())

		var out bytes.Buffer

		cmd.SetOut(&out)

		require.NoError(t, cmd.RunE(cmd, nil))
		assert.Contains(t, out.String(), "Deletion request sent")
		assert.Contains(t, out.String(), "Telemetry disabled")
	})

	t.Run("force-enabled keeps telemetry on", func(t *testing.T) {
		t.Parallel()

		cmd := newResetCmd(makeResetProps(logger.NewNoop(), nil, true))
		cmd.SetContext(context.Background())

		var out bytes.Buffer

		cmd.SetOut(&out)

		require.NoError(t, cmd.RunE(cmd, nil))
		assert.Contains(t, out.String(), "remains enabled")
	})

	t.Run("deletion failure is reported but not fatal", func(t *testing.T) {
		t.Parallel()

		p := makeResetProps(logger.NewNoop(), errors.New("network down"), false)
		p.Tool.Help = props.SlackHelp{Team: "team", Channel: "ops"}
		cmd := newResetCmd(p)
		cmd.SetContext(context.Background())

		var out bytes.Buffer

		cmd.SetOut(&out)

		require.NoError(t, cmd.RunE(cmd, nil))
		assert.Contains(t, out.String(), "could not be sent")
		assert.Contains(t, out.String(), "For manual deletion", "support message must be shown on failure")
	})
}
