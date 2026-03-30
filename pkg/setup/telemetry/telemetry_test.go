package telemetry

import (
	"testing"

	"github.com/charmbracelet/huh"
	testifymock "github.com/stretchr/testify/mock"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func newTestProps(t *testing.T) *props.Props {
	t.Helper()

	return &props.Props{
		Tool:   props.Tool{Name: "test-tool"},
		Logger: logger.NewNoop(),
	}
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

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().IsSet("telemetry.enabled").Return(true)

	init := NewTelemetryInitialiser(newTestProps(t))

	if !init.IsConfigured(mock) {
		t.Error("IsConfigured should return true when telemetry.enabled is set")
	}
}

func TestTelemetryInitialiser_IsConfigured_EnvSet(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("TELEMETRY_ENABLED", "true")

	mock := mockcfg.NewMockContainable(t)

	init := NewTelemetryInitialiser(newTestProps(t))

	if !init.IsConfigured(mock) {
		t.Error("IsConfigured should return true when TELEMETRY_ENABLED is set")
	}
}

func TestTelemetryInitialiser_IsConfigured_Neither(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().IsSet("telemetry.enabled").Return(false)

	init := NewTelemetryInitialiser(newTestProps(t))

	if init.IsConfigured(mock) {
		t.Error("IsConfigured should return false when neither config nor env is set")
	}
}

func TestTelemetryInitialiser_Configure_EnvTrue(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("TELEMETRY_ENABLED", "true")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().Set("telemetry.enabled", true).Return()

	p := newTestProps(t)
	init := NewTelemetryInitialiser(p)

	if err := init.Configure(p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}

func TestTelemetryInitialiser_Configure_EnvFalse(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("TELEMETRY_ENABLED", "false")

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().Set("telemetry.enabled", false).Return()

	p := newTestProps(t)
	init := NewTelemetryInitialiser(p)

	if err := init.Configure(p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}

func TestTelemetryInitialiser_Configure_FormOptIn(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().Set("telemetry.enabled", testifymock.Anything).Run(func(key string, value any) {
		if v, ok := value.(bool); !ok || !v {
			t.Errorf("expected telemetry.enabled = true, got %v", value)
		}
	}).Return()

	p := newTestProps(t)
	init := NewTelemetryInitialiser(p, WithForm(func(_ *props.Props, optIn *bool) *huh.Form {
		*optIn = true

		return nil // skip form rendering
	}))

	if err := init.Configure(p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}

func TestTelemetryInitialiser_Configure_FormOptOut(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.EXPECT().Set("telemetry.enabled", testifymock.Anything).Run(func(key string, value any) {
		if v, ok := value.(bool); !ok || v {
			t.Errorf("expected telemetry.enabled = false, got %v", value)
		}
	}).Return()

	p := newTestProps(t)
	init := NewTelemetryInitialiser(p, WithForm(func(_ *props.Props, optIn *bool) *huh.Form {
		*optIn = false

		return nil // skip form rendering
	}))

	if err := init.Configure(p, mock); err != nil {
		t.Fatalf("Configure error: %v", err)
	}
}
