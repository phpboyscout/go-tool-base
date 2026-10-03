package generate

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// summaryGroup is the wizard's last page: every answer, then a confirm, so
// nothing is generated or applied without the author seeing what will be.
func (o *SkeletonOptions) summaryGroup() *huh.Group {
	o.confirmed = true

	title, act := "Ready to generate", "Generate now"
	if o.revisit {
		title, act = "Ready to apply", "Apply now"
	}

	return huh.NewGroup(
		huh.NewNote().Title("Summary").Description(o.wizardSummary()).
			DescriptionFunc(o.wizardSummary, textBinding{o.wizardSummary}),
		huh.NewConfirm().Key("confirm").Title(act+"?").
			Description("Cancel leaves everything as it was; shift+tab goes back to change an answer.").
			Affirmative(act).Negative("Cancel").Value(&o.confirmed),
	).Title(title)
}

// confirmedOrCancelled is the summary's answer as runWizard returns it: a
// Cancel is the same as ctrl+c.
func (o *SkeletonOptions) confirmedOrCancelled() error {
	if !o.confirmed {
		return huh.ErrUserAborted
	}

	return nil
}

// wizardSummary lists the answers as they stand, resolving the ones the
// wizard only settles afterwards (the env prefix, the source slots).
func (o *SkeletonOptions) wizardSummary() string {
	var rows [][2]string

	add := func(label, value string) {
		if value != "" {
			rows = append(rows, [2]string{label, value})
		}
	}

	add("Project", o.Name)
	add("Description", o.Description)
	add("Hosting", o.hostingSummary())
	add("Features", strings.Join(o.Features, ", "))
	add("Env prefix", cmp.Or(o.summaryEnvPrefix(), "none"))

	if o.updateSelected() {
		add("Self-update", strings.Join(nonEmpty(o.ReleaseChannel, o.UpdatePolicy, o.UpdateCheckInterval), ", "))
		add("Signing", map[bool]string{true: "on, key source " + o.SigningKeySource, false: "off"}[o.Signing])
	}

	add("Chat providers", strings.Join(o.ChatProviders, ", "))
	add("Chat default", o.ChatDefault.Provider)

	if slices.Contains(o.Features, string(props.TelemetryCmd)) {
		add("Telemetry", cmp.Or(o.TelemetryEndpoint, "framework default"))
	}

	if slices.Contains(o.Features, string(props.McpCmd)) {
		add("MCP mode", o.MCPMode)
	}

	if o.HelpType != "" && o.HelpType != "none" {
		add("Help channel", o.HelpType)
	}

	add("Config formats", "yaml"+prefixed(", ", strings.Join(o.ConfigFormats, ", ")))
	add("Own config file", cmp.Or(o.ConfigFormat, "yaml"))
	add("Config sources", cmp.Or(o.sourcesSummary(), "none"))

	width := 0
	for _, r := range rows {
		width = max(width, len(r[0]))
	}

	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = fmt.Sprintf("%-*s  %s", width, r[0], r[1])
	}

	return noteText(strings.Join(lines, "\n"))
}

func (o *SkeletonOptions) hostingSummary() string {
	if !o.hosted {
		return "not hosted; module " + cmp.Or(o.Module, o.Name)
	}

	private := ""
	if o.Private {
		private = ", private"
	}

	return strings.Join(nonEmpty(o.ForgeBackend, o.Host, o.Repo), " ") + private
}

func (o *SkeletonOptions) summaryEnvPrefix() string {
	switch o.envPrefixChoice {
	case envPrefixDerived:
		return deriveEnvPrefix(o.Name)
	case envPrefixNone:
		return ""
	default:
		return o.EnvPrefix
	}
}

func (o *SkeletonOptions) sourcesSummary() string {
	if o.sourceSlots == nil {
		return strings.Join(o.ConfigSources, ", ")
	}

	var out []string

	for i, s := range o.sourceSlots {
		if s.Kind == "" {
			break
		}

		if !o.kindOffered(s.Kind) {
			continue
		}

		detail := s.Kind
		if !s.Required {
			detail += ", optional"
		}

		out = append(out, fmt.Sprintf("%s (%s)", o.slotName(i), detail))
	}

	return strings.Join(out, ", ")
}

func prefixed(prefix, value string) string {
	if value == "" {
		return ""
	}

	return prefix + value
}

func nonEmpty(values ...string) []string {
	return slices.DeleteFunc(values, func(v string) bool { return v == "" })
}
