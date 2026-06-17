package verifier

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	chatmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// These exercise the LegacyVerifier's real-toolchain path (go build/test,
// golangci-lint as subprocesses), so they are gated behind the generator-build
// integration tags rather than run on every `go test`.

// TestLegacyVerifier_VerifyGeneratedCode_Integration runs the toolchain against
// an invalid (empty, non-module) directory and asserts the verification errors
// are collected rather than swallowed.
func TestLegacyVerifier_VerifyGeneratedCode_Integration(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	v := NewLegacy(&props.Props{Logger: logger.NewNoop(), FS: afero.NewOsFs()}, t.TempDir())

	errs := v.verifyGeneratedCode(context.Background())
	assert.NotEmpty(t, errs, "an empty, non-module directory must fail compilation/verification")
}

// TestLegacyVerifier_VerifyAndFix_NoAIClient_Integration drives the full
// VerifyAndFix loop against an invalid project with no AI client: verification
// fails, there is nothing to repair, so the loop breaks and the verifier
// returns nil (best-effort) after logging a caution.
func TestLegacyVerifier_VerifyAndFix_NoAIClient_Integration(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	projectRoot := t.TempDir()
	v := NewLegacy(&props.Props{Logger: logger.NewNoop(), FS: afero.NewOsFs()}, projectRoot)

	gen := func(_ context.Context, _ string, _ *templates.CommandData) error { return nil }

	err := v.VerifyAndFix(context.Background(), projectRoot, projectRoot, &templates.CommandData{}, nil, gen)
	require.NoError(t, err, "VerifyAndFix is best-effort: it returns nil even when verification fails with no AI fixer")
}

// TestLegacyVerifier_VerifyAndFix_WithAIClient_Integration exercises the
// AI-repair branch: verification keeps failing (empty project), so each retry
// asks the AI to fix the code and cleans up before regenerating. The stub
// "fixes" nothing, so the loop exhausts its retries and returns nil best-effort.
func TestLegacyVerifier_VerifyAndFix_WithAIClient_Integration(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	projectRoot := t.TempDir()
	v := NewLegacy(&props.Props{Logger: logger.NewNoop(), FS: afero.NewOsFs()}, projectRoot)

	m := chatmocks.NewMockChatClient(t)
	m.EXPECT().Ask(mock.Anything, mock.Anything, mock.Anything).Return(nil)

	gen := func(_ context.Context, _ string, _ *templates.CommandData) error { return nil }

	err := v.VerifyAndFix(context.Background(), projectRoot, projectRoot, &templates.CommandData{}, m, gen)
	require.NoError(t, err)
}
