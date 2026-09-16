package generator

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGoVersion_IsTheManifests pins spec 0197 D3: regenerate renders the go
// directive the manifest records, not the toolchain the author happens to
// run today; generate records what it resolved; an older manifest without
// the field gains the running toolchain once, and keeps it.
func TestGoVersion_IsTheManifests(t *testing.T) {
	t.Parallel()

	m := Manifest{Version: ManifestVersion{GoToolBase: "v1", Go: "1.26"}}
	assert.Equal(t, "1.26", buildSkeletonTemplateDataFrom(m).GoVersion, "regenerate reads version.go")

	recorded := manifestFromSkeletonConfig(SkeletonConfig{Name: "t", Repo: "o/t", Host: "github.com"}, nil, "v1")
	assert.Equal(t, strings.TrimPrefix(runtime.Version(), "go"), recorded.Version.Go, "generate records the resolved version, not an empty override")

	older := &Manifest{Properties: ManifestProperties{Name: "t"}}
	changed, err := deriveMissingManifestFields(older)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, strings.TrimPrefix(runtime.Version(), "go"), older.Version.Go, "an older manifest gains the field")

	changed, err = deriveMissingManifestFields(older)
	require.NoError(t, err)
	assert.False(t, changed, "and keeps it on the next run")
}
