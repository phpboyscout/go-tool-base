package forge

import (
	"context"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/internal/formpage"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// runPages runs a wizard's pages through setup.RunForm: one form on a
// terminal, and in accessible mode only the visible pages (see formpage).
func runPages(ctx context.Context, p *props.Props, pages []formpage.Page) error {
	return formpage.Run(pages, p.GetIO().Accessible(), huh.NewForm, func(f *huh.Form) error {
		return setup.RunForm(ctx, p, f)
	})
}
