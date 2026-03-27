package changelog

import (
	"fmt"
	"strings"
)

// FormatSummary produces a human-readable summary of the changelog suitable for
// terminal output. Breaking changes appear first with a warning prefix.
func FormatSummary(cl *Changelog) string {
	if cl == nil || len(cl.Releases) == 0 {
		return ""
	}

	var sb strings.Builder

	if cl.HasBreakingChanges() {
		sb.WriteString("WARNING: Breaking changes detected!\n\n")

		for _, e := range cl.BreakingChanges() {
			if e.Scope != "" {
				fmt.Fprintf(&sb, "  BREAKING: %s: %s\n", e.Scope, e.Description)
			} else {
				fmt.Fprintf(&sb, "  BREAKING: %s\n", e.Description)
			}
		}

		sb.WriteString("\n")
	}

	categories := []struct {
		category Category
		label    string
	}{
		{CategoryFeature, "Features"},
		{CategoryFix, "Bug Fixes"},
		{CategoryPerformance, "Performance"},
		{CategoryOther, "Other"},
	}

	for _, cat := range categories {
		entries := cl.EntriesByCategory(cat.category)
		if len(entries) == 0 {
			continue
		}

		fmt.Fprintf(&sb, "%s:\n", cat.label)

		for _, e := range entries {
			if e.Scope != "" {
				fmt.Fprintf(&sb, "  - %s: %s\n", e.Scope, e.Description)
			} else {
				fmt.Fprintf(&sb, "  - %s\n", e.Description)
			}
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
