package gomod

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/mod/modfile"
)

func TestRequirements_BuildInfoWhenNoFloor(t *testing.T) {
	t.Parallel()

	got := Requirements([]string{"github.com/spf13/cobra"}, MapSource{"github.com/spf13/cobra": "v1.10.2"}, nil)
	assert.Equal(t, []Requirement{{Path: "github.com/spf13/cobra", Version: "v1.10.2"}}, got)
}

func TestSeed_RejectsAMalformedGoVersion(t *testing.T) {
	t.Parallel()

	_, _, err := Seed(nil, nil, nil, WithModule("github.com/acme/mytool", "one.two"))
	require.ErrorContains(t, err, "add go directive")
}

func TestIsDevelopmentReplace_NoSyntaxIsNotOurs(t *testing.T) {
	t.Parallel()

	assert.False(t, isDevelopmentReplace(&modfile.Replace{}))
}

func TestAnnotateReplace_LeavesTextWithoutAReplaceAlone(t *testing.T) {
	t.Parallel()

	in := []byte("module x\n")
	assert.Equal(t, in, annotateReplace(in))
	assert.Equal(t, []byte("// GTB_FRAMEWORK_REPLACE\nreplace a => b\n"), annotateReplace([]byte("// GTB_FRAMEWORK_REPLACE\nreplace a => b\n")), "already annotated")
}
