package docs

import (
	"fmt"
	"io/fs"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/browser"
	docslib "gitlab.com/phpboyscout/go-tool-base/pkg/docs"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	defaultPort = 8080

	// defaultHost is the loopback interface the docs server binds to unless
	// the operator widens it with --host. Loopback keeps the locally-served
	// static site off the network by default.
	defaultHost = "127.0.0.1"
)

// NewCmdDocsServe creates the docs serve subcommand for local documentation hosting.
func NewCmdDocsServe(_ *props.Props, efs fs.FS) *cobra.Command {
	var (
		port int
		host string
		open bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve documentation as a static site",
		Long:  "Start a local HTTP server and serve the documentation as a Material-styled static site.",
		// RunE (not Run) so the command flows through the recovery/timing/
		// telemetry middleware chain wired by Command.Register, and surfaces
		// errors via the standard ErrorHandler path like every other built-in.
		RunE: func(cmd *cobra.Command, _ []string) error {
			subFS, err := fs.Sub(efs, "assets/site")
			if err != nil {
				return errors.Wrap(err, "failed to load static site assets")
			}

			if open && port != 0 {
				go func() {
					// If port is 0 the bound port is only known inside Serve,
					// so we skip auto-open in that case.
					url := fmt.Sprintf("http://%s:%d", host, port)
					_ = browser.OpenURL(cmd.Context(), url)
				}()
			}

			return docslib.Serve(cmd.Context(), subFS, port, docslib.WithHost(host))
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", defaultPort, "Port to listen on (0 for random)")
	cmd.Flags().StringVar(&host, "host", defaultHost,
		"Interface to bind (use 0.0.0.0 to expose on all interfaces — trusted networks only)")
	cmd.Flags().BoolVar(&open, "open", true, "Automatically open the browser")

	return cmd
}
