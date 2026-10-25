package verifier

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	chatmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func testProps(fs afero.Fs) *props.Props {
	return &props.Props{Logger: logger.NewNoop(), FS: fs}
}

// --- AgentVerifier ---------------------------------------------------------

func TestNewAgentVerifier(t *testing.T) {
	t.Parallel()

	v := NewAgentVerifier(testProps(afero.NewMemMapFs()), true)
	require.NotNil(t, v)
	assert.True(t, v.allowQuery)
}

func newAgentTest(t *testing.T) (*AgentVerifier, *chatmocks.MockChatClient) {
	t.Helper()

	return NewAgentVerifier(testProps(afero.NewMemMapFs()), false), chatmocks.NewMockChatClient(t)
}

func TestAgentVerifier_VerifyAndFix_Success(t *testing.T) {
	t.Parallel()

	v, m := newAgentTest(t)
	m.EXPECT().SetTools(mock.Anything).Return(nil)
	m.EXPECT().Chat(mock.Anything, mock.Anything).Return("All checks pass. SUCCESS", nil)

	err := v.VerifyAndFix(context.Background(), "/root", "/root/cmd", &templates.CommandData{}, m, nil)
	require.NoError(t, err)
}

func TestAgentVerifier_VerifyAndFix_FailureReturnsErrVerificationFailed(t *testing.T) {
	t.Parallel()

	v, m := newAgentTest(t)
	m.EXPECT().SetTools(mock.Anything).Return(nil)
	m.EXPECT().Chat(mock.Anything, mock.Anything).Return("FAILURE: could not fix the build", nil)

	err := v.VerifyAndFix(context.Background(), "/root", "/root/cmd", &templates.CommandData{}, m, nil)
	require.ErrorIs(t, err, ErrVerificationFailed)
	assert.Contains(t, err.Error(), "could not fix the build")
}

func TestAgentVerifier_VerifyAndFix_SetToolsError(t *testing.T) {
	t.Parallel()

	v, m := newAgentTest(t)
	m.EXPECT().SetTools(mock.Anything).Return(errors.New("no tools"))

	err := v.VerifyAndFix(context.Background(), "/root", "/root/cmd", &templates.CommandData{}, m, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set tools")
}

func TestAgentVerifier_VerifyAndFix_ChatError(t *testing.T) {
	t.Parallel()

	v, m := newAgentTest(t)
	m.EXPECT().SetTools(mock.Anything).Return(nil)
	m.EXPECT().Chat(mock.Anything, mock.Anything).Return("", errors.New("api down"))

	err := v.VerifyAndFix(context.Background(), "/root", "/root/cmd", &templates.CommandData{}, m, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent chat failed")
}

// TestAgentVerifier_VerifyAndFix_InteractiveSucceeds covers the allowQuery=true
// branch, which registers the query_user tool and uses the interactive
// clarification prompt. The huh form itself is never invoked (the mock returns
// SUCCESS without a tool call), so no TTY is required.
func TestAgentVerifier_VerifyAndFix_InteractiveSucceeds(t *testing.T) {
	t.Parallel()

	v := NewAgentVerifier(testProps(afero.NewMemMapFs()), true)
	m := chatmocks.NewMockChatClient(t)
	m.EXPECT().SetTools(mock.Anything).Return(nil)
	m.EXPECT().Chat(mock.Anything, mock.Anything).Return("SUCCESS", nil)

	err := v.VerifyAndFix(context.Background(), "/root", "/root/cmd", &templates.CommandData{}, m, nil)
	require.NoError(t, err)
}
