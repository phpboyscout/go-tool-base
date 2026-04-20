// Package doctor provides a diagnostic command that validates configuration,
// checks environment health, and reports runtime details.
package doctor

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
)

// CheckStatus is the outcome of a diagnostic check.
type CheckStatus = string

const (
	CheckPass CheckStatus = "pass"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
	CheckSkip CheckStatus = "skip"
)

// CheckResult is an alias for the registry's CheckResult type.
type CheckResult = setup.CheckResult

// CheckFunc is an alias for the registry's CheckFunc type.
type CheckFunc = setup.CheckFunc

// DoctorReport contains all check results.
type DoctorReport struct {
	Tool    string        `json:"tool"`
	Version string        `json:"version"`
	Checks  []CheckResult `json:"checks"`
}

// NewCmdDoctor creates the doctor command.
func NewCmdDoctor(props *p.Props) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check environment and configuration health",
		Long:  "Run diagnostic checks to validate configuration, connectivity, and runtime environment.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("output")
			out := output.NewWriter(os.Stdout, output.Format(format))

			report := RunChecks(cmd.Context(), props)

			return out.Write(output.Response{
				Status:  output.StatusSuccess,
				Command: "doctor",
				Data:    report,
			}, func(w io.Writer) {
				PrintReport(w, report)
			})
		},
	}

	return cmd
}

// RunChecks executes all diagnostic checks and returns a report.
func RunChecks(ctx context.Context, props *p.Props) *DoctorReport {
	report := &DoctorReport{
		Tool: props.Tool.Name,
	}

	if props.Version != nil {
		report.Version = props.Version.GetVersion()
	}

	// Run built-in checks
	for _, check := range DefaultChecks() {
		report.Checks = append(report.Checks, check(ctx, props))
	}

	// Run registered checks from the feature registry
	for _, check := range discoverChecks(props) {
		report.Checks = append(report.Checks, check(ctx, props))
	}

	return report
}

// discoverChecks returns all registered check functions for enabled features.
func discoverChecks(props *p.Props) []CheckFunc {
	var checks []CheckFunc

	for feature, providers := range setup.GetChecks() {
		if props.Tool.IsEnabled(feature) {
			for _, provider := range providers {
				checks = append(checks, provider(props)...)
			}
		}
	}

	return checks
}

// DefaultChecks returns the standard set of diagnostic checks.
func DefaultChecks() []CheckFunc {
	return []CheckFunc{
		checkGoVersion,
		checkConfig,
		checkGit,
		checkAPIKeys,
		checkNoLiteralCredentials,
		checkPermissions,
	}
}

// PrintReport writes a human-readable report to the given writer.
func PrintReport(w io.Writer, report *DoctorReport) {
	_, _ = fmt.Fprintf(w, "%s %s\n\n", report.Tool, report.Version)

	for _, check := range report.Checks {
		var icon string

		switch check.Status {
		case CheckPass:
			icon = "[OK]"
		case CheckWarn:
			icon = "[!!]"
		case CheckFail:
			icon = "[FAIL]"
		case CheckSkip:
			icon = "[SKIP]"
		}

		_, _ = fmt.Fprintf(w, "  %s %s: %s\n", icon, check.Name, check.Message)

		if check.Details != "" {
			_, _ = fmt.Fprintf(w, "       %s\n", check.Details)
		}
	}
}
