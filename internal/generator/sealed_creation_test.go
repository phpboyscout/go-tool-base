package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Issue #17: spec 0188 D3 says a sealed path is "never rendered, never wired,
// never created and never deleted by the generator". Creation was the case it
// did not cover — handleExecutionFile gated only on whether the file existed
// and on --force, and never consulted the ignore rules at all. Sealing a
// main.go and deleting it got the file recreated on the next regeneration.
//
// Its sibling branch in the same function was already correct: when main.go is
// preserved, ensureHookStubs guards with wiringSealed. Only the create branch
// was missing the check.
//
// wiringSealed is the right predicate rather than IsIgnored, because 0188 D2
// deliberately lets a *plain* ignore rule through here. Refusing to create
// main.go leaves the rendered cmd.go referencing a RunX the package never
// defines, which is a hard compile error — so only the explicit `sealed`
// attribute, where the developer has accepted that consequence, stops it.

func sealedCreationFixture(t *testing.T, ignoreFile string) (*Generator, string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	root := "/work"
	cmdDir := filepath.Join(root, "pkg", "cmd", "lexicon")

	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), DefaultDirMode))

	if ignoreFile != "" {
		require.NoError(t, afero.WriteFile(fs,
			filepath.Join(root, ".gtb", "ignore"), []byte(ignoreFile), DefaultFileMode))
	}

	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	g := New(p, &Config{Path: root, Name: "lexicon"})

	return g, cmdDir
}

func executionData() *templates.CommandData {
	return &templates.CommandData{
		Name:       "lexicon",
		PascalName: "Lexicon",
		Hashes:     map[string]string{},
	}
}

func TestHandleExecutionFile_SealedPathIsNotCreated(t *testing.T) {
	t.Parallel()

	g, cmdDir := sealedCreationFixture(t, "pkg/cmd/lexicon/main.go sealed\n")
	mainFile := filepath.Join(cmdDir, "main.go")

	require.NoError(t, g.handleExecutionFile(context.Background(), cmdDir, executionData()))

	exists, err := afero.Exists(g.props.FS, mainFile)
	require.NoError(t, err)
	assert.False(t, exists, "a sealed path must never be created (0188 D3)")

	assert.Contains(t, g.conflicts.sealed, "pkg/cmd/lexicon/main.go",
		"the refusal must be recorded so the end-of-run summary reports it (0188 D6)")
}

func TestHandleExecutionFile_SealedPathIsNotCreatedUnderForce(t *testing.T) {
	t.Parallel()

	// .gtb/ignore outranks a flag on one invocation — the precedence
	// signing_goreleaser.go and resolveConflict already state.
	g, cmdDir := sealedCreationFixture(t, "pkg/cmd/lexicon/main.go sealed\n")
	g.config.Force = true

	require.NoError(t, g.handleExecutionFile(context.Background(), cmdDir, executionData()))

	exists, err := afero.Exists(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.False(t, exists, "--force must not override a seal")
}

func TestHandleExecutionFile_PlainIgnoreRuleStillCreates(t *testing.T) {
	t.Parallel()

	// 0188 D2: a plain rule blocks rendering but not the wiring writes whose
	// refusal breaks the build. cmd.go references RunLexicon, so a missing
	// main.go does not compile — this file has to be written.
	g, cmdDir := sealedCreationFixture(t, "pkg/cmd/lexicon/main.go\n")

	require.NoError(t, g.handleExecutionFile(context.Background(), cmdDir, executionData()))

	exists, err := afero.Exists(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.True(t, exists, "a plain ignore rule must not leave the package uncompilable")
}

func TestHandleExecutionFile_UnruledPathIsCreated(t *testing.T) {
	t.Parallel()

	g, cmdDir := sealedCreationFixture(t, "")

	require.NoError(t, g.handleExecutionFile(context.Background(), cmdDir, executionData()))

	exists, err := afero.Exists(g.props.FS, filepath.Join(cmdDir, "main.go"))
	require.NoError(t, err)
	assert.True(t, exists)
}
