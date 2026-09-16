package telemetry

import (
	"io"
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"

	propstest "gitlab.com/phpboyscout/go-tool-base/pkg/props/test"

	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"

	testifymock "github.com/stretchr/testify/mock"

	mockcfg "gitlab.com/phpboyscout/go/config/mocks"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// consentIO answers the consent question at an accessible prompt.
func consentIO(answer string) props.IO {
	return props.StdIO{Stdin: formtest.Answers(answer), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true}
}

func newTestProps(t *testing.T) *props.Props {
	t.Helper()

	return propstest.New(propstest.WithTool(props.Tool{Name: "test-tool"}))
}

func TestTelemetryInitialiser_Name(t *testing.T) {
	t.Parallel()

	init := NewTelemetryInitialiser(newTestProps(t))

	if init.Name() != "telemetry" {
		t.Errorf("Name() = %q, want %q", init.Name(), "telemetry")
	}
}

func TestTelemetryInitialiser_IsConfigured_KeySet(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockReader(t)
	mock.EXPECT().IsSet("telemetry.enabled").Return(true)

	init := NewTelemetryInitialiser(newTestProps(t))

	if !init.IsConfigured(mock) {
		t.Error("IsConfigured should return true when telemetry.enabled is set")
	}
}

func TestTelemetryInitialiser_IsConfigured_EnvSet(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("TELEMETRY_ENABLED", "true")

	mock := mockcfg.NewMockReader(t)

	init := NewTelemetryInitialiser(newTestProps(t))

	if !init.IsConfigured(mock) {
		t.Error("IsConfigured should return true when TELEMETRY_ENABLED is set")
	}
}

func TestTelemetryInitialiser_IsConfigured_Neither(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockReader(t)
	mock.EXPECT().IsSet("telemetry.enabled").Return(false)

	init := NewTelemetryInitialiser(newTestProps(t))

	if init.IsConfigured(mock) {
		t.Error("IsConfigured should return false when neither config nor env is set")
	}
}

func TestTelemetryInitialiser_Configure_EnvTrue(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("TELEMETRY_ENABLED", "true")

	mock := setupmocks.NewMockEditor(t)
	mock.EXPECT().Set("telemetry.enabled", true).Return(nil)

	p := newTestProps(t)
	init := NewTelemetryInitialiser(p)

	if err := init.Configure(t.Context(), p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}

func TestTelemetryInitialiser_Configure_EnvFalse(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("TELEMETRY_ENABLED", "false")

	mock := setupmocks.NewMockEditor(t)
	mock.EXPECT().Set("telemetry.enabled", false).Return(nil)

	p := newTestProps(t)
	init := NewTelemetryInitialiser(p)

	if err := init.Configure(t.Context(), p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}

func TestTelemetryInitialiser_Configure_FormOptIn(t *testing.T) {
	t.Parallel()

	mock := setupmocks.NewMockEditor(t)
	mock.EXPECT().Set("telemetry.enabled", testifymock.Anything).Run(func(key string, value any) {
		if v, ok := value.(bool); !ok || !v {
			t.Errorf("expected telemetry.enabled = true, got %v", value)
		}
	}).Return(nil)

	p := newTestProps(t)
	p.IO = consentIO("y")
	init := NewTelemetryInitialiser(p)

	if err := init.Configure(t.Context(), p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}

func TestTelemetryInitialiser_Configure_FormOptOut(t *testing.T) {
	t.Parallel()

	mock := setupmocks.NewMockEditor(t)
	mock.EXPECT().Set("telemetry.enabled", testifymock.Anything).Run(func(key string, value any) {
		if v, ok := value.(bool); !ok || v {
			t.Errorf("expected telemetry.enabled = false, got %v", value)
		}
	}).Return(nil)

	p := newTestProps(t)
	p.IO = consentIO("n")
	init := NewTelemetryInitialiser(p)

	if err := init.Configure(t.Context(), p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}
