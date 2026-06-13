package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// newConfigProbeCmd builds a contrived E2E fixture command that proves the
// framework bootstrap (config load, telemetry, feature flags, update check —
// in the ROOT command's PersistentPreRunE) still runs even when a downstream
// subcommand defines its OWN PersistentPreRunE.
//
// Before EnableTraverseRunHooks was set in NewCmdRootWithConfig, a child's
// PersistentPreRunE shadowed the root's, so props.Config was never populated.
// This command:
//
//   - defines its own PersistentPreRunE that prints a marker to stdout
//     ("config-probe: child hook ran"), proving the child hook fires; and
//   - in RunE reads "probe.value" from props.Config and prints it as
//     "config-probe: value=<value>".
//
// If the root bootstrap ran (root→leaf traversal), props.Config is populated
// from the --config file and the value prints. If the bootstrap had been
// shadowed by the child hook (the old bug), the config would be empty and the
// value would print blank.
//
// This is a test-only fixture; it is only ever linked into cmd/e2e, never the
// shipped gtb binary.
func newConfigProbeCmd(p *props.Props) *setup.Command {
	cmd := &cobra.Command{
		Use:   "config-probe",
		Short: "E2E fixture: prove root bootstrap survives a child PersistentPreRunE",
		// A child-defined PersistentPreRunE — the exact shape that used to
		// shadow the root's bootstrap. EnableTraverseRunHooks guarantees the
		// root's hook still runs before this one.
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Write to real stdout (cobra's Print* default to stderr) so the
			// E2E harness, which captures stdout and stderr separately, can
			// assert on it.
			fmt.Println("config-probe: child hook ran")

			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			// props.Config is populated by the root's PersistentPreRunE. If the
			// bootstrap had been shadowed, this would be the zero value.
			value := p.Config.GetString("probe.value")
			fmt.Printf("config-probe: value=%s\n", value)

			return nil
		},
	}

	return setup.Wrap("", cmd)
}
