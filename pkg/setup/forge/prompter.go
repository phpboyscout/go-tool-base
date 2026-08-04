package forge

import (
	"context"
	"fmt"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go/browser"
	"gitlab.com/phpboyscout/go/errors"
	forgeapi "gitlab.com/phpboyscout/go/forge"
)

// cliPrompter is the CLI-side [forgeapi.Prompter]: it renders an OAuth
// device-code prompt through huh (a note carrying the verification URL and user
// code, plus a confirmation to continue) and best-effort opens the verification
// URL through pkg/browser.OpenURL (scheme-allowlisted). It is how pkg/setup
// surfaces a [forgeapi.Authenticator]'s interactive step while presentation
// stays out of the forge module — the provider speaks only the OAuth protocol.
//
// The huh form runner and the browser opener are injected as seams so the
// prompt is exercisable without a TTY: tests substitute a fake runner (asserting
// the built form) and a fake opener (asserting the completing URL is used and
// that a failed open is non-fatal).
type cliPrompter struct {
	run    func(*huh.Form) error
	opener func(context.Context, string) error
}

// newCLIPrompter builds the default prompter: renders the device-code note via
// huh and opens URLs via browser.OpenURL (which validates the URL against a
// scheme allowlist).
func newCLIPrompter() *cliPrompter {
	return &cliPrompter{
		run: func(f *huh.Form) error { return f.Run() },
		opener: func(ctx context.Context, u string) error {
			return browser.OpenURL(ctx, u)
		},
	}
}

// deviceCodeMessage is the human-facing body of the device-code note: the plain
// verification URL (short enough to retype on any device) and the user code.
// Factored out so tests can assert the rendered content without driving a TTY.
func deviceCodeMessage(dc forgeapi.DeviceCode) string {
	return fmt.Sprintf(
		"Open this URL to authorize access:\n\n  %s\n\nEnter this code: %s",
		dc.VerificationURI, dc.UserCode)
}

// ShowDeviceCode implements [forgeapi.Prompter]. It best-effort opens the
// completing verification URL (which pre-fills the code), then renders a huh
// note showing the plain URL and user code with a confirmation to continue. A
// failed open (e.g. a headless server with no browser) is deliberately
// non-fatal: the user can open the printed URL on any device and type the code,
// which is the whole point of the device flow.
func (c *cliPrompter) ShowDeviceCode(ctx context.Context, dc forgeapi.DeviceCode) error {
	// Prefer the completing URL (pre-fills the code) for the browser; the note
	// below shows the plain URL so it is short enough to retype.
	target := dc.VerificationURIComplete
	if target == "" {
		target = dc.VerificationURI
	}

	if c.opener != nil {
		_ = c.opener(ctx, target)
	}

	if c.run == nil {
		return nil
	}

	proceed := true
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Authorize access").
				Description(deviceCodeMessage(dc)),
			huh.NewConfirm().
				Title("Continue once you have entered the code?").
				Affirmative("Continue").
				Negative("Cancel").
				Value(&proceed),
		),
	)

	if err := c.run(form); err != nil {
		return errors.Wrap(err, "device-code prompt cancelled")
	}

	return nil
}
