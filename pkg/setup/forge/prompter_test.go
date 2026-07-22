package forge

import (
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeapi "gitlab.com/phpboyscout/go/forge"
)

// TestCLIPrompter_ShowDeviceCode_OpensCompleteURL asserts the huh note is
// rendered (via the injected runner), the note body carries the code and plain
// URL, and the browser is opened at the completing URL (which pre-fills the
// code).
func TestCLIPrompter_ShowDeviceCode_OpensCompleteURL(t *testing.T) {
	t.Parallel()

	var (
		openedT string
		ranForm bool
	)

	p := &cliPrompter{
		run: func(f *huh.Form) error {
			ranForm = f != nil

			return nil
		},
		opener: func(_ context.Context, u string) error {
			openedT = u

			return nil
		},
	}

	dc := forgeapi.DeviceCode{
		UserCode:                "WDJB-MJHT",
		VerificationURI:         "https://github.com/login/device",
		VerificationURIComplete: "https://github.com/login/device?user_code=WDJB-MJHT",
	}
	require.NoError(t, p.ShowDeviceCode(t.Context(), dc))

	assert.True(t, ranForm, "the device-code form must be rendered via the runner")

	// The rendered note carries the user code and the plain verification URL.
	msg := deviceCodeMessage(dc)
	assert.Contains(t, msg, "WDJB-MJHT")
	assert.Contains(t, msg, "https://github.com/login/device")

	// The browser is opened at the completing URL (pre-fills the code), while
	// the plain URL is the one shown for manual entry.
	assert.Equal(t, "https://github.com/login/device?user_code=WDJB-MJHT", openedT)
}

// TestCLIPrompter_ShowDeviceCode_FallsBackToPlainURL — with no completing URL,
// the browser is opened at the plain verification URL.
func TestCLIPrompter_ShowDeviceCode_FallsBackToPlainURL(t *testing.T) {
	t.Parallel()

	var openedT string

	p := &cliPrompter{
		run: func(*huh.Form) error { return nil },
		opener: func(_ context.Context, u string) error {
			openedT = u

			return nil
		},
	}

	require.NoError(t, p.ShowDeviceCode(t.Context(), forgeapi.DeviceCode{
		UserCode:        "ABCD-1234",
		VerificationURI: "https://github.com/login/device",
	}))
	assert.Equal(t, "https://github.com/login/device", openedT)
}

// TestCLIPrompter_ShowDeviceCode_OpenErrorIsNonFatal — a failed browser open
// (e.g. headless server) must not fail the prompt: the code and URL are shown
// for the user to open on any device.
func TestCLIPrompter_ShowDeviceCode_OpenErrorIsNonFatal(t *testing.T) {
	t.Parallel()

	p := &cliPrompter{
		run: func(*huh.Form) error { return nil },
		opener: func(context.Context, string) error {
			return assert.AnError
		},
	}

	require.NoError(t, p.ShowDeviceCode(t.Context(), forgeapi.DeviceCode{
		UserCode:        "X",
		VerificationURI: "https://github.com/login/device",
	}))
}

// TestCLIPrompter_ShowDeviceCode_RunErrorPropagates — cancelling the huh
// confirmation surfaces as an error the login flow can act on.
func TestCLIPrompter_ShowDeviceCode_RunErrorPropagates(t *testing.T) {
	t.Parallel()

	p := &cliPrompter{
		run: func(*huh.Form) error { return assert.AnError },
		opener: func(context.Context, string) error {
			return nil
		},
	}

	err := p.ShowDeviceCode(t.Context(), forgeapi.DeviceCode{
		UserCode:        "X",
		VerificationURI: "https://github.com/login/device",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "device-code prompt cancelled")
}

// TestNewCLIPrompter_Defaults confirms the production prompter wires both seams.
func TestNewCLIPrompter_Defaults(t *testing.T) {
	t.Parallel()

	p := newCLIPrompter()
	require.NotNil(t, p)
	assert.NotNil(t, p.run)
	assert.NotNil(t, p.opener)
}
