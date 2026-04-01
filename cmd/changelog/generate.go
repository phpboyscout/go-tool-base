package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/changelog"
)

const filePerm = 0o644

func newGenerateCmd() *cobra.Command {
	var (
		output     string
		sinceTag   string
		releases   int
		includeAll bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a changelog from git history",
		Long: `Generate a CHANGELOG.md from conventional commits in the git history.

Reads the repository at the current directory, resolves version tags,
parses conventional commit messages, and outputs grouped markdown.

If --output is specified and the file already exists, only missing
releases are appended. Manually edited content is preserved.`,
		Run: func(cmd *cobra.Command, _ []string) {
			var opts []changelog.GenerateOption

			if sinceTag != "" {
				opts = append(opts, changelog.WithSinceTag(sinceTag))
			}

			if releases > 0 {
				opts = append(opts, changelog.WithMaxReleases(releases))
			}

			if includeAll {
				opts = append(opts, changelog.WithIncludeAll())
			}

			result, err := changelog.GenerateFromRepo(".", opts...)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: changelog generation skipped: %v\n", err)

				return
			}

			if output == "" {
				fmt.Print(result)

				return
			}

			if err := os.WriteFile(output, []byte(result), filePerm); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not write changelog: %v\n", err)

				return
			}

			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Changelog written to %s\n", output)
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Write changelog to file instead of stdout")
	cmd.Flags().StringVar(&sinceTag, "since", "", "Generate only for releases after this tag")
	cmd.Flags().IntVar(&releases, "releases", 0, "Limit to N most recent releases (0 = all)")
	cmd.Flags().BoolVar(&includeAll, "include-all", false, "Include non-conventional commits under Other")

	return cmd
}
