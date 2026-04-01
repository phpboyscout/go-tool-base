// Package changelog provides the changelog command for displaying version
// history from an embedded CHANGELOG.md.
package changelog

import (
	"io"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/changelog"
	"github.com/phpboyscout/go-tool-base/pkg/output"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

const changelogAssetPath = "assets/CHANGELOG.md"

// NewCmdChangelog creates the changelog command that displays version history
// from the embedded assets. The CHANGELOG.md must be included in the tool's
// props.Assets under any mount point. Returns nil if no assets are configured.
func NewCmdChangelog(p *props.Props) *cobra.Command {
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

			cl := changelog.Parse(raw)

			releases := filterReleases(cl, version, since, latest)

			return renderOutput(cmd, p, releases)
		},
	}

	cmd.Flags().StringVar(&version, "version", "", "Show changes for a specific version (e.g. v1.2.0)")
	cmd.Flags().StringVar(&since, "since", "", "Show changes since a version (exclusive)")
	cmd.Flags().BoolVar(&latest, "latest", false, "Show only the most recent release")

	return cmd
}

// loadChangelog reads CHANGELOG.md from the tool's embedded assets.
// Returns a helpful error if the changelog is not available.
func loadChangelog(p *props.Props) (string, error) {
	if p.Assets == nil {
		return "", errors.New("no changelog available — assets not configured")
	}

	f, err := p.Assets.Open(changelogAssetPath)
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

func filterReleases(cl *changelog.Changelog, version, since string, latest bool) []changelog.Release {
	if latest && len(cl.Releases) > 0 {
		return cl.Releases[len(cl.Releases)-1:]
	}

	if version != "" {
		for _, r := range cl.Releases {
			if r.Version == version {
				return []changelog.Release{r}
			}
		}

		return nil
	}

	if since != "" {
		var filtered []changelog.Release

		var found bool

		for _, r := range cl.Releases {
			if found {
				filtered = append(filtered, r)
			}

			if r.Version == since {
				found = true
			}
		}

		return filtered
	}

	return cl.Releases
}

func renderOutput(cmd *cobra.Command, p *props.Props, releases []changelog.Release) error {
	if len(releases) == 0 {
		p.Logger.Print("No matching changelog entries found.")

		return nil
	}

	if output.IsJSONOutput(cmd) {
		return output.Emit(cmd, output.Response{
			Status:  output.StatusSuccess,
			Command: "changelog",
			Data:    releases,
		})
	}

	// Text output — render as markdown
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

	rendered := output.RenderMarkdown(sb.String())
	p.Logger.Print(rendered)

	return nil
}
