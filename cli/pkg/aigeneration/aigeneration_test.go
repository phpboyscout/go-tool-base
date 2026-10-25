package aigeneration

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func accessibleProps(answers ...string) *props.Props {
	return &props.Props{Logger: logger.NewNoop(), IO: formtest.AccessibleTTY(formtest.Answers(answers...))}
}

// TestConfigure pins spec 0211 D5: gtb init asks, the default is No, and
// either answer is written so the person can see the setting.
func TestConfigure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		answer string
		want   bool
	}{
		{name: "the default is no", answer: "", want: false},
		{name: "yes", answer: "y", want: true},
		{name: "no", answer: "n", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			editor := setupmocks.NewMockEditor(t)
			editor.EXPECT().Set(generator.ConfigKeyGeneratorAI, tc.want).Return(nil)

			p := accessibleProps(tc.answer)
			require.NoError(t, Initialiser{}.Configure(t.Context(), p, editor))
		})
	}
}

func TestIsConfigured(t *testing.T) {
	t.Parallel()

	assert.False(t, Initialiser{}.IsConfigured(testutil.StoreFromYAML(t, "ai:\n  provider: claude\n").View()))
	assert.True(t, Initialiser{}.IsConfigured(testutil.StoreFromYAML(t, "generator:\n  ai: false\n").View()))
}

// TestRegisteredUnderAI: the question rides the AI feature and is skipped
// wherever --skip-ai skips the AI initialiser.
func TestRegisteredUnderAI(t *testing.T) {
	t.Setenv("CI", "")

	snapshot := features.Default().Snapshot()

	var ours []setup.InitialiserProvider

	for _, ip := range setup.InitialisersIn(snapshot)[props.AiCmd] {
		if _, ok := ip(&props.Props{Logger: logger.NewNoop()}, nil).(Initialiser); ok {
			ours = append(ours, ip)
		}
	}

	require.Len(t, ours, 1)

	cmd := &cobra.Command{Use: "init"}
	cmd.Flags().Bool("skip-ai", false, "")
	require.NoError(t, cmd.Flags().Set("skip-ai", "true"))
	assert.Nil(t, ours[0](&props.Props{Logger: logger.NewNoop()}, cmd.Flags()))
}
