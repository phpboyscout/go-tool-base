package forge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

var deviceCode = forgeapi.DeviceCode{
	UserCode:                "WDJB-MJHT",
	VerificationURI:         "https://github.com/login/device",
	VerificationURIComplete: "https://github.com/login/device?user_code=WDJB-MJHT",
}

// TestCLIPrompter_ShowDeviceCode_OpensCompleteURL asserts the note shown
// carries the code and plain URL, and the browser is opened at the completing
// URL (which pre-fills the code).
func TestCLIPrompter_ShowDeviceCode_OpensCompleteURL(t *testing.T) {
	t.Parallel()

	var opened string

	p := newTestProps(t)
	io, out := answersIO("y")
	p.IO = io

	c := newCLIPrompter(p)
	c.opener = func(_ context.Context, u string) error {
		opened = u

		return nil
	}

	require.NoError(t, c.ShowDeviceCode(t.Context(), deviceCode))

	// The note shown carries the user code and the plain verification URL.
	assert.Contains(t, out.String(), "WDJB-MJHT")
	assert.Contains(t, out.String(), "https://github.com/login/device")
	assert.Contains(t, out.String(), "Continue once you have entered the code?")

	// The browser is opened at the completing URL (pre-fills the code), while
	// the plain URL is the one shown for manual entry.
	assert.Equal(t, "https://github.com/login/device?user_code=WDJB-MJHT", opened)
}

// TestCLIPrompter_ShowDeviceCode_FallsBackToPlainURL: with no completing URL,
// the browser is opened at the plain verification URL.
func TestCLIPrompter_ShowDeviceCode_FallsBackToPlainURL(t *testing.T) {
	t.Parallel()

	var opened string

	p := newTestProps(t)
	p.IO, _ = answersIO("y")

	c := newCLIPrompter(p)
	c.opener = func(_ context.Context, u string) error {
		opened = u

		return nil
	}

	dc := deviceCode
	dc.VerificationURIComplete = ""

	require.NoError(t, c.ShowDeviceCode(t.Context(), dc))
	assert.Equal(t, "https://github.com/login/device", opened)
}

// TestCLIPrompter_ShowDeviceCode_OpenErrorIsNonFatal: a headless server with
// no browser still gets the note, and the flow continues.
func TestCLIPrompter_ShowDeviceCode_OpenErrorIsNonFatal(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	p.IO, _ = answersIO("y")

	c := newCLIPrompter(p)
	c.opener = func(context.Context, string) error { return assert.AnError }

	require.NoError(t, c.ShowDeviceCode(t.Context(), deviceCode))
}

// TestCLIPrompter_ShowDeviceCode_RefusesANonInteractiveRun: with nobody at
// the terminal the prompt cannot be answered, and the login is cancelled.
func TestCLIPrompter_ShowDeviceCode_RefusesANonInteractiveRun(t *testing.T) {
	t.Parallel()

	p := newTestProps(t)
	p.IO = nonInteractiveIO()

	c := newCLIPrompter(p)
	c.opener = func(context.Context, string) error { return nil }

	err := c.ShowDeviceCode(t.Context(), deviceCode)
	require.ErrorIs(t, err, setup.ErrNonInteractive)
	assert.Contains(t, err.Error(), "device-code prompt cancelled")
}

// TestNewCLIPrompter_Defaults confirms the production prompter opens URLs
// through the allowlisted browser opener.
func TestNewCLIPrompter_Defaults(t *testing.T) {
	t.Parallel()

	c := newCLIPrompter(newTestProps(t))
	require.NotNil(t, c)
	assert.NotNil(t, c.opener)
	assert.NotNil(t, c.p)
}
