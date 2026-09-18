// Package changelog provides the `changelog` command for displaying version
// history from an embedded CHANGELOG.md, with optional version and since-tag
// filtering. It is a no-op when no changelog asset is embedded.
package changelog

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/spf13/cobra"

	"gitlab.com/phpboyscout/go/changelog"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go/output"
	ocobra "gitlab.com/phpboyscout/go/output/cobra"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

const changelogAssetPath = "assets/CHANGELOG.md"

// NewCmdChangelog creates the changelog command that displays version history
// from the embedded assets. The CHANGELOG.md must be included in the tool's
// props.Assets under any mount point. Returns nil if no assets are configured.
func NewCmdChangelog(p *props.Props) *setup.Command {
	var version string

	var since string

	var latest bool

	cmd := &cobra.Command{
		Use:   "changelog",
		Short: "Show version history",
		Long: `Display the changelog for this tool. The changelog is embedded at build time
and always reflects the version you are running.

By default, shows the full changelog. Use flags to filter by version.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := loadChangelog(p)
			if err != nil {
				return err
			}

			cl, err := changelog.Parse(raw)
			if err != nil {
				return err
			}

			releases := filterReleases(cl, version, since, latest)

			return renderOutput(cmd, releases)
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "Show changes for a specific version (e.g. v1.2.0)")
	cmd.Flags().StringVar(&since, "since", "", "Show changes since a version (exclusive)")
	cmd.Flags().BoolVar(&latest, "latest", false, "Show only the most recent release")

	return setup.AnnotateMCP(setup.Wrap(props.ChangelogCmd, cmd), setup.MCPReadOnly())
}

// loadChangelog reads CHANGELOG.md from the tool's embedded assets.
// Returns a helpful error if the changelog is not available.
func loadChangelog(p props.AssetProvider) (string, error) {
	assets := p.GetAssets()
	if assets == nil {
		return "", errors.New("no changelog available — assets not configured")
	}

	f, err := assets.Open(changelogAssetPath)
	if err != nil {
		return "", errors.New("no changelog available — CHANGELOG.md not found in embedded assets")
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(f)
	if err != nil {
		return "", errors.Wrap(err, "reading CHANGELOG.md from assets")
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", errors.New("changelog is empty")
	}

	return content, nil
}

// filterReleases selects the releases to show, newest first. The parser
// yields them oldest-first (the order the file is written in); a reader of a
// changelog wants the current release at the top.
func filterReleases(cl *changelog.Changelog, version, since string, latest bool) []changelog.Release {
	releases := newestFirst(cl.Releases)

	if latest && len(releases) > 0 {
		return releases[:1]
	}

	if version != "" {
		for _, r := range releases {
			if r.Version == version {
				return []changelog.Release{r}
			}
		}

		return nil
	}

	if since != "" {
		// Everything newer than since, which in newest-first order is
		// everything before it.
		for i, r := range releases {
			if r.Version == since {
				return releases[:i]
			}
		}

		return nil
	}

	return releases
}

func newestFirst(releases []changelog.Release) []changelog.Release {
	out := make([]changelog.Release, len(releases))
	for i, r := range releases {
		out[len(releases)-1-i] = r
	}

	return out
}

// releaseView is the command's wire shape for a release: snake_case keys and
// the category by name, rather than the parser's struct with an iota.
type releaseView struct {
	Version string      `json:"version"`
	Entries []entryView `json:"entries"`
}

type entryView struct {
	Category string `json:"category"`
	Scope    string `json:"scope,omitempty"`
	Text     string `json:"description"`
}

var categoryNames = map[changelog.Category]string{
	changelog.CategoryBreaking:    "breaking",
	changelog.CategoryFeature:     "feature",
	changelog.CategoryFix:         "fix",
	changelog.CategoryPerformance: "performance",
	changelog.CategoryOther:       "other",
}

func viewOf(releases []changelog.Release) []releaseView {
	out := make([]releaseView, 0, len(releases))

	for _, r := range releases {
		v := releaseView{Version: r.Version, Entries: make([]entryView, 0, len(r.Entries))}
		for _, e := range r.Entries {
			v.Entries = append(v.Entries, entryView{Category: categoryNames[e.Category], Scope: e.Scope, Text: e.Description})
		}

		out = append(out, v)
	}

	return out
}

func renderOutput(cmd *cobra.Command, releases []changelog.Release) error {
	if len(releases) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No matching changelog entries found.")

		return nil
	}

	if ocobra.IsJSONOutput(cmd) {
		return ocobra.Emit(cmd, output.Response{
			Status:  output.StatusSuccess,
			Command: "changelog",
			Data:    viewOf(releases),
		})
	}

	var sb strings.Builder

	for _, r := range releases {
		sb.WriteString("## " + r.Version + "\n\n")

		for _, e := range r.Entries {
			if e.Scope != "" {
				sb.WriteString("- **" + e.Scope + ":** " + e.Description + "\n")
			} else {
				sb.WriteString("- " + e.Description + "\n")
			}
		}

		sb.WriteString("\n")
	}

	// Styled through glamour for a person at a terminal; plain Markdown for a
	// pipe, which would otherwise receive one escape sequence per word.
	out := cmd.OutOrStdout()
	if isTerminal(out) {
		_, _ = fmt.Fprintln(out, output.RenderMarkdown(sb.String()))

		return nil
	}

	_, _ = fmt.Fprint(out, sb.String())

	return nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)

	return ok && term.IsTerminal(int(f.Fd()))
}
