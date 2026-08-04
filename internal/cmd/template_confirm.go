package cmd

import (
	"os"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// ErrRemoteTemplateDeclined is returned when the operator declines the
// first-use trust confirmation for a remote template source.
var ErrRemoteTemplateDeclined = errors.NewSentinel("gtb.cmd.remote_template_declined", "remote template source not confirmed")

// ConfirmRemoteTemplate runs the first-use trust confirmation for a remote
// (git) template source. Adding a remote source IS the trust decision — GTB
// renders template content the framework did not author, so the operator must
// explicitly accept the host/owner/repo and ref before it is fetched.
//
// Local sources need no confirmation (the operator already owns the path). In
// a non-interactive session (CI, --ci, GTB_NON_INTERACTIVE) the prompt is
// skipped and the source is accepted: supplying --template in CI is itself the
// explicit acceptance, mirroring the signing wizard's CI behaviour.
func ConfirmRemoteTemplate(p *props.Props, ci bool, ts generator.TemplateSource) error {
	if ts.Type != generator.TemplateSourceGit {
		return nil
	}

	if ci || !utils.IsInteractive() || os.Getenv("GTB_NON_INTERACTIVE") == "true" {
		p.Logger.Warn("trusting remote template source (non-interactive)", "location", ts.Location, "ref", refOrDefault(ts.Ref))

		return nil
	}

	confirmed := false

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Trust this remote template source?").
				Description(remoteTemplateWarning(ts)).
				Affirmative("Yes, fetch and apply").
				Negative("No, abort").
				Value(&confirmed),
		),
	)

	if err := form.Run(); err != nil {
		return errors.Wrap(err, "remote template confirmation failed")
	}

	if !confirmed {
		return errors.Wrapf(ErrRemoteTemplateDeclined, "%q", ts.Location)
	}

	return nil
}

// remoteTemplateWarning renders the confirmation body naming exactly what is
// being trusted.
func remoteTemplateWarning(ts generator.TemplateSource) string {
	return "gtb will fetch and render template content from:\n\n  " +
		ts.Location + " @ " + refOrDefault(ts.Ref) + "\n\n" +
		"Custom templates are executed as trusted input — their output correctness " +
		"and safety are the template author's responsibility. Only proceed if you " +
		"trust this source."
}

func refOrDefault(ref string) string {
	if ref == "" {
		return "(default branch)"
	}

	return ref
}
