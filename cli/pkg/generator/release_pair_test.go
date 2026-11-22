package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	oldGoreleaser = "release:\n  use_existing_draft: true\n  mode: keep-existing\n"
	newGoreleaser = "release:\n  disable: true\nuploads:\n  - name: gitlab-generic\n"
	oldGitlabCI   = "include:\n  - component: gitlab.com/phpboyscout/cicd/colophon@v1\n    inputs:\n      branch: main\n"
	newGitlabCI   = "include:\n  - component: gitlab.com/phpboyscout/cicd/go-test@v1\n" +
		"  - component: gitlab.com/phpboyscout/cicd/colophon@v1\n    inputs:\n      assets: true\n      asset_base: https://x\n"
)

const releasePairWarning = "kept file is on the old release flow"

func TestReportReleasePair(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		goreleaser, gitlabCI string
		kept, ignored        []string
		warnsFor             string
	}{
		"kept old goreleaser beside a rewritten CI file": {
			goreleaser: oldGoreleaser, gitlabCI: newGitlabCI,
			kept: []string{goreleaserFile}, warnsFor: goreleaserFile,
		},
		"kept old CI file beside a rewritten goreleaser": {
			goreleaser: newGoreleaser, gitlabCI: oldGitlabCI,
			kept: []string{gitlabCIFile}, warnsFor: gitlabCIFile,
		},
		"an ignored file is held back too": {
			goreleaser: oldGoreleaser, gitlabCI: newGitlabCI,
			ignored: []string{goreleaserFile}, warnsFor: goreleaserFile,
		},
		"both kept, so the two still agree": {
			goreleaser: oldGoreleaser, gitlabCI: oldGitlabCI,
			kept: []string{goreleaserFile, gitlabCIFile},
		},
		"the kept file was already migrated by hand": {
			goreleaser: newGoreleaser, gitlabCI: newGitlabCI,
			kept: []string{goreleaserFile},
		},
		"no GitLab CI file, so not a GitLab project": {
			goreleaser: oldGoreleaser,
			kept:       []string{goreleaserFile},
		},
		"a kept old CI file whose include mixes strings and maps": {
			goreleaser: newGoreleaser,
			gitlabCI:   "include:\n  - 'ci/extra.yml'\n  - component: gitlab.com/phpboyscout/cicd/colophon@v1\n    inputs:\n      branch: main\n",
			kept:       []string{gitlabCIFile}, warnsFor: gitlabCIFile,
		},
		"a kept old CI file with a single-map include": {
			goreleaser: newGoreleaser,
			gitlabCI:   "include:\n  component: gitlab.com/phpboyscout/cicd/colophon@v1\n  inputs:\n    branch: main\n",
			kept:       []string{gitlabCIFile}, warnsFor: gitlabCIFile,
		},
		"files disagree, but this run held back only an unrelated file": {
			goreleaser: oldGoreleaser, gitlabCI: newGitlabCI,
			kept: []string{"pkg/cmd/alpha/cmd.go"},
		},
		"neither held back": {
			goreleaser: newGoreleaser, gitlabCI: newGitlabCI,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, fs, buf := newPerimeterTestProject(t, "properties:\n  name: mytool\n")
			writeIfSet(t, fs, "/work/"+goreleaserFile, tc.goreleaser)
			writeIfSet(t, fs, "/work/"+gitlabCIFile, tc.gitlabCI)

			for _, rel := range tc.kept {
				g.conflicts.recordKeep(rel, "test")
			}

			for _, rel := range tc.ignored {
				g.conflicts.recordIgnored(rel)
			}

			g.reportConflicts()

			if tc.warnsFor == "" {
				assert.False(t, buf.ContainsLevel(logger.WarnLevel, releasePairWarning), buf.Messages())

				return
			}

			require.True(t, buf.ContainsLevel(logger.WarnLevel, releasePairWarning), buf.Messages())
			assert.Equal(t, tc.warnsFor, attrValue(t, buf, releasePairWarning, "path"))
			assert.Equal(t, releasePairChange[tc.warnsFor], attrValue(t, buf, releasePairWarning, "change"))
		})
	}
}

func writeIfSet(t *testing.T, fs afero.Fs, path, body string) {
	t.Helper()

	if body != "" {
		require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
	}
}

// The detection must read gtb's own rendered release files as the new flow,
// on both channels, or every GitLab project would be warned about.
func TestAttachesAssetsAtTag_RenderedSkeleton(t *testing.T) {
	t.Parallel()

	for _, channel := range []string{ReleaseChannelForge, ReleaseChannelStatic} {
		t.Run(channel, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			g := newSkeletonGeneratorForTest(t, fs)
			g.config.Path = "/work"
			require.NoError(t, g.GenerateSkeleton(context.Background(), gitlabReleaseConfig(channel)))

			for _, f := range []string{goreleaserFile, gitlabCIFile} {
				attaches, ok := g.attachesAssetsAtTag(f)
				assert.True(t, ok, f)
				assert.True(t, attaches, f)
			}
		})
	}
}
