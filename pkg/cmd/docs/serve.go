package docs

import (
	"fmt"
	"io/fs"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/browser"
	docslib "github.com/phpboyscout/go-tool-base/pkg/docs"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	defaultPort = 8080
)

// NewCmdDocsServe creates the docs serve subcommand for local documentation hosting.
func NewCmdDocsServe(p *props.Props, efs fs.FS) *cobra.Command {
	var (
		port int
		open bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve documentation as a static site",
		Long:  "Start a local HTTP server and serve the documentation as a Material-styled static site.",
		Run: func(cmd *cobra.Command, args []string) {
			subFS, err := fs.Sub(efs, "assets/site")
			if err != nil {
				p.ErrorHandler.Fatal(errors.Newf("failed to load static site assets: %w", err))
			}

			if open {
				go func() {
					// Give the server a moment to start
					url := fmt.Sprintf("http://localhost:%d", port)
					if port == 0 {
						// If port is 0, we can't easily know it here without changing Serve signature
						// but Serve already logs it.
						return
					}

					_ = browser.OpenURL(cmd.Context(), url)
				}()
			}

			if err := docslib.Serve(cmd.Context(), subFS, port); err != nil {
				p.ErrorHandler.Fatal(err)
			}
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", defaultPort, "Port to listen on (0 for random)")
	cmd.Flags().BoolVar(&open, "open", true, "Automatically open the browser")

	return cmd
}
