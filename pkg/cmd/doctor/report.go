package doctor

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/osinfo"
	"gitlab.com/phpboyscout/go-tool-base/pkg/output"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/redact"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// SupportBundle is the fully-collected, already-redacted output of
// `doctor report`. Every string field and every value in Config has passed
// through pkg/redact before CollectBundle returns it. It subsumes a plain
// `doctor` run by embedding the DoctorReport.
type SupportBundle struct {
	Tool     ToolSection    `json:"tool"`
	Runtime  RuntimeSection `json:"runtime"`
	Paths    PathsSection   `json:"paths"`
	Features []FeatureFlag  `json:"features"`
	Config   map[string]any `json:"config"` // redacted, key-name-preserving
	Doctor   *DoctorReport  `json:"doctor,omitempty"`
}

// ToolSection holds the tool's identity and build metadata.
type ToolSection struct {
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

// RuntimeSection holds the Go runtime and host OS/arch.
type RuntimeSection struct {
	Go   string `json:"go"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// PathsSection holds the resolved config locations (no cache dir — GTB has no
// cache subsystem).
type PathsSection struct {
	ConfigDir  string `json:"config_dir,omitempty"`
	ConfigFile string `json:"config_file,omitempty"`
}

// FeatureFlag is one built-in feature's enabled/disabled state.
type FeatureFlag struct {
	Cmd     p.FeatureCmd `json:"cmd"`
	Enabled bool         `json:"enabled"`
}

// NewCmdReport returns the `doctor report` subcommand: it prints a single,
// secret-redacted, paste-ready support bundle. It is gated implicitly by being
// a child of the DoctorCmd-gated `doctor` command.
func NewCmdReport(props *p.Props) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Print a redacted, paste-ready support bundle",
		Long: `Collect a single, secret-redacted support bundle — tool and runtime
versions, the resolved configuration (credentials stripped), config paths,
feature-flag state, and the full doctor report — ready to paste straight into a
GitLab/GitHub issue.

Where plain "doctor" gives a health verdict, "doctor report" adds the state dump
(config, paths, flags) around it. The entire bundle is redacted before it is
written, in both text and JSON form; there is no option to disable redaction.
For a raw value, read the specific config key directly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("output")
			out := output.NewWriter(cmd.OutOrStdout(), output.Format(format))
			bundle := CollectBundle(cmd.Context(), props)

			return out.Write(output.Response{
				Status:  output.StatusSuccess,
				Command: "doctor report",
				Data:    bundle,
			}, func(w io.Writer) { PrintBundle(w, bundle) })
		},
	}
}

// CollectBundle gathers the support bundle and applies the redaction perimeter.
// It is total: nil Config or nil Version yield empty/omitted sections, never a
// panic.
func CollectBundle(ctx context.Context, props *p.Props) *SupportBundle {
	bundle := &SupportBundle{
		Tool: ToolSection{
			Name:    props.Tool.Name,
			Summary: props.Tool.Summary,
		},
		Runtime: RuntimeSection{
			Go:   runtime.Version(),
			OS:   osinfo.Version(),
			Arch: runtime.GOARCH,
		},
	}

	if props.Version != nil {
		bundle.Tool.Version = props.Version.GetVersion()
		bundle.Tool.Commit = props.Version.GetCommit()
		bundle.Tool.Date = props.Version.GetDate()
	}

	if props.FS != nil {
		if dir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name); dir != "" {
			bundle.Paths.ConfigDir = redact.String(dir)
		}
	}

	for _, cmd := range p.AllFeatures {
		bundle.Features = append(bundle.Features, FeatureFlag{
			Cmd:     cmd,
			Enabled: props.Tool.IsEnabled(cmd),
		})
	}

	if props.Config != nil {
		if v := props.Config.GetViper(); v != nil {
			bundle.Config = redactConfig(v.AllSettings())

			if used := v.ConfigFileUsed(); used != "" {
				bundle.Paths.ConfigFile = redact.String(used)
			}
		}
	}

	bundle.Doctor = collectDoctor(ctx, props)

	return bundle
}

// collectDoctor reuses RunChecks verbatim, then re-redacts each result's
// free-form message/details. Built-in checks already report key-names-only, but
// the check set is extensible (downstream features register their own via
// discoverChecks) and those messages carry no such guarantee — so this pass is
// defense-in-depth, not redundancy. It is cheap and idempotent (redact
// guarantees String(String(s)) == String(s)).
func collectDoctor(ctx context.Context, props *p.Props) *DoctorReport {
	rep := RunChecks(ctx, props)
	if rep == nil {
		return nil
	}

	for i := range rep.Checks {
		rep.Checks[i].Message = redact.String(rep.Checks[i].Message)
		rep.Checks[i].Details = redact.String(rep.Checks[i].Details)
	}

	return rep
}

// PrintBundle renders the human-readable bundle: a header, then labelled
// sections, reusing PrintReport for the checks for visual consistency.
func PrintBundle(w io.Writer, b *SupportBundle) {
	_, _ = fmt.Fprintf(w, "%s %s\n", b.Tool.Name, b.Tool.Version)

	if b.Tool.Summary != "" {
		_, _ = fmt.Fprintf(w, "%s\n", b.Tool.Summary)
	}

	_, _ = fmt.Fprintln(w, "\nRuntime:")
	_, _ = fmt.Fprintf(w, "  go:     %s\n", b.Runtime.Go)
	_, _ = fmt.Fprintf(w, "  os:     %s\n", b.Runtime.OS)
	_, _ = fmt.Fprintf(w, "  arch:   %s\n", b.Runtime.Arch)

	if b.Tool.Commit != "" {
		_, _ = fmt.Fprintf(w, "  commit: %s\n", b.Tool.Commit)
	}

	if b.Tool.Date != "" {
		_, _ = fmt.Fprintf(w, "  built:  %s\n", b.Tool.Date)
	}

	_, _ = fmt.Fprintln(w, "\nPaths:")
	printPath(w, "config_dir", b.Paths.ConfigDir)
	printPath(w, "config_file", b.Paths.ConfigFile)

	_, _ = fmt.Fprintln(w, "\nFeatures:")

	for _, f := range b.Features {
		state := "disabled"
		if f.Enabled {
			state = "enabled"
		}

		_, _ = fmt.Fprintf(w, "  %-10s %s\n", f.Cmd, state)
	}

	_, _ = fmt.Fprintln(w, "\nConfig (redacted):")
	printConfig(w, b.Config)

	if b.Doctor != nil {
		_, _ = fmt.Fprintln(w, "\nChecks:")
		PrintReport(w, b.Doctor)
	}
}

func printPath(w io.Writer, label, value string) {
	if value == "" {
		value = "(not set)"
	}

	_, _ = fmt.Fprintf(w, "  %-12s %s\n", label+":", value)
}

// printConfig renders the redacted config map as indented YAML (sorted keys).
func printConfig(w io.Writer, cfg map[string]any) {
	if len(cfg) == 0 {
		_, _ = fmt.Fprintln(w, "  (none)")

		return
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(w, "  (unrenderable)")

		return
	}

	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		_, _ = fmt.Fprintf(w, "  %s\n", line)
	}
}
