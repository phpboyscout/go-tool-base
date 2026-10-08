package generate

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// runGroupAccessible runs one wizard page as line prompts. An answer its
// validator refuses is asked again, so each page is fed a refused answer
// before the one it keeps.
func runGroupAccessible(t *testing.T, g *huh.Group, answers ...string) {
	t.Helper()

	p := &props.Props{IO: formtest.AccessibleTTY(formtest.Answers(answers...))}
	require.NoError(t, runForm(context.Background(), p, newForm(g)))
}

// renderGroup focuses the page in a TUI model, runs the commands huh returns
// so its reactive descriptions, placeholders and options are evaluated, and
// renders it.
func renderGroup(t *testing.T, g *huh.Group) string {
	t.Helper()

	f := newForm(g)
	pump(f, f.Init(), pumpDepth)

	return f.View()
}

const (
	pumpDepth = 6
	// cmdPatience bounds each command: huh's cursor-blink ticks block, and a
	// reactive binding's command returns at once.
	cmdPatience = 50 * time.Millisecond
)

// pump runs cmd and feeds what it produces back into the model, to depth.
func pump(m huh.Model, cmd tea.Cmd, depth int) huh.Model {
	if cmd == nil || depth == 0 {
		return m
	}

	done := make(chan tea.Msg, 1)

	go func() { done <- cmd() }()

	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(cmdPatience):
		return m
	}

	switch msg := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range msg {
			m = pump(m, c, depth-1)
		}

		return m
	default:
		next, nextCmd := m.Update(msg)

		return pump(next, nextCmd, depth-1)
	}
}

func TestWizardPages_ValidatorsRefuseInPlace(t *testing.T) {
	t.Parallel()

	t.Run("slack", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{HelpType: "slack"}
		runGroupAccessible(t, o.slackGroup(), "", "bad\x00", "#help", "bad\x00", "Platform")
		assert.Equal(t, "#help", o.SlackChannel)
		assert.Equal(t, "Platform", o.SlackTeam)
	})

	t.Run("teams", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{HelpType: "teams"}
		runGroupAccessible(t, o.teamsGroup(), "", "bad\x00", "Support", "bad\x00", "Engineering")
		assert.Equal(t, "Support", o.TeamsChannel)
		assert.Equal(t, "Engineering", o.TeamsTeam)
	})

	t.Run("forge", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{hosted: true, ForgeBackend: "gitlab"}
		runGroupAccessible(t, o.forgeGroup(), "", "", "", "org//repo", "org/repo", "y", "0")
		assert.Equal(t, "org/repo", o.Repo)
		assert.True(t, o.Private)
	})

	t.Run("custom env prefix", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{envPrefixChoice: envPrefixOther}
		runGroupAccessible(t, o.envPrefixCustomGroup(), "", "1FOO", "MY_APP")
		assert.Equal(t, "MY_APP", o.EnvPrefix)
	})

	t.Run("module", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool"}
		runGroupAccessible(t, o.moduleGroup(), "bad module", "example.com/tool")
		assert.Equal(t, "example.com/tool", o.Module)

		o = &SkeletonOptions{Name: "tool"}
		runGroupAccessible(t, o.moduleGroup(), "")
		assert.Empty(t, o.Module, "empty is accepted and filled from the name after the wizard")
	})
}

func TestSelfUpdatePage_ChannelValidation(t *testing.T) {
	t.Parallel()

	t.Run("a hosted project takes the forge channel", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{hosted: true, ForgeBackend: "github", Features: []string{string(props.UpdateCmd)}}
		runGroupAccessible(t, o.selfUpdateGroup(), "1", "", "")
		assert.Equal(t, generator.ReleaseChannelForge, o.ReleaseChannel)
	})

	t.Run("an unhosted project is offered only the static location", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{string(props.UpdateCmd)}}
		runGroupAccessible(t, o.selfUpdateGroup(), "1", "", "")
		assert.Equal(t, generator.ReleaseChannelStatic, o.ReleaseChannel)
	})
}

func TestWizardPages_RenderTheirReactiveText(t *testing.T) {
	t.Parallel()

	t.Run("forge page names the backend's host", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{hosted: true, ForgeBackend: "gitlab"}
		assert.Contains(t, renderGroup(t, o.forgeGroup()), "Leave empty for gitlab.com")
	})

	t.Run("module page names the project", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool"}
		assert.Contains(t, renderGroup(t, o.moduleGroup()), "The module line of go.mod")
	})

	t.Run("self-update page names the forge", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{hosted: true, ForgeBackend: "github", Features: []string{string(props.UpdateCmd)}}
		assert.Contains(t, renderGroup(t, o.selfUpdateGroup()), "GitHub releases (github.com)")
	})

	t.Run("env prefix page derives from the name", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "my-tool"}
		assert.Contains(t, renderGroup(t, o.envPrefixGroup()), "MY_TOOL")
	})

	t.Run("chat default page lists the providers", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{"ai"}, ChatProviders: []string{"claude", "openai"}}
		assert.Contains(t, renderGroup(t, o.chatDefaultGroup()), "openai")
	})
}

func TestWizardHelpers_RemainingBranches(t *testing.T) {
	t.Parallel()

	t.Run("chat preselect defaults under ai", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, generator.DefaultChatProviders(), (&SkeletonOptions{Features: []string{"ai"}}).chatPreselect())
		assert.Nil(t, (&SkeletonOptions{}).chatPreselect())
	})

	t.Run("a forge feature has a generated gloss", func(t *testing.T) {
		t.Parallel()

		for _, d := range forgeBackendDisplays() {
			assert.NotEmpty(t, featureGloss(string(d.ID)), d.ID)
		}
	})

	t.Run("the hosting backend is not also a credential forge", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", hosted: true, ForgeBackend: "github", ForgeCredentials: []string{"github", "gitlab"}}
		require.NoError(t, o.afterWizard())
		assert.Equal(t, []string{"gitlab"}, o.ForgeCredentials)
	})

	t.Run("the default key source is stored empty", func(t *testing.T) {
		t.Parallel()

		got := (&SkeletonOptions{Signing: true, SigningKeySource: "both"}).resolveSigning()
		assert.True(t, got.Enabled)
		assert.Empty(t, got.KeySource)
	})

	t.Run("a help channel is summarised", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", HelpType: "slack"}
		assert.Contains(t, o.wizardSummary(), "Help channel")
	})

	t.Run("sources are summarised from the flags before the page loads", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{ConfigSources: []string{"team=env", "vault=vault"}}
		assert.Equal(t, "team=env, vault=vault", o.sourcesSummary())
	})

	t.Run("slots already loaded are kept", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{sourceSlots: []wizardSource{{Kind: "env", Name: "team"}}}
		o.prepareSourceSlots()
		assert.Equal(t, []wizardSource{{Kind: "env", Name: "team"}}, o.sourceSlots)
	})

	t.Run("every slot filled is every slot active", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{sourceSlots: []wizardSource{{Kind: "env"}, {Kind: "keychain"}}}
		assert.Len(t, o.activeSourceSlots(), 2)
	})

	t.Run("a read-only slot is recorded", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{}
		o.recordSourceSlots([]wizardSource{
			{Kind: "env", Name: "team", Required: true, Access: sourceAccessReadOnly},
			{Kind: "keychain", Name: "keys", Required: true, Access: sourceAccessWritable},
		})
		assert.Equal(t, []string{"team"}, o.readOnlySources)
		assert.Equal(t, []string{"keys"}, o.ConfigSourcesWritable)
		assert.Equal(t, []string{"team=env", "keys=keychain"}, o.ConfigSources)
	})

	t.Run("a source slot's page renders its settings", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Name: "tool", sourceSlots: []wizardSource{{Kind: "keychain", Required: true}}}
		assert.Contains(t, renderGroup(t, o.sourceDetailGroup(0)), "Settings")
	})

	t.Run("private hosting is summarised", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{hosted: true, ForgeBackend: "github", Repo: "org/tool", Private: true}
		assert.Equal(t, "github org/tool, private", o.hostingSummary())
	})
}

// The tool bridge page asks only when a local CLI is linked, and a No is
// recorded as the opt-out (#104).
func TestChatToolBridgePage(t *testing.T) {
	t.Parallel()

	t.Run("No opts out", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{string(props.AiCmd)}, ChatProviders: []string{"claude-local"}}
		o.seedChatToolBridge()
		runGroupAccessible(t, o.chatToolBridgeGroup(), "n")
		require.NoError(t, o.afterWizard())

		cfg := o.skeletonConfig(nil)
		require.NotNil(t, cfg.Chat.ToolBridge)
		assert.False(t, *cfg.Chat.ToolBridge)
	})

	t.Run("Yes keeps the default rule", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Features: []string{string(props.AiCmd)}, ChatProviders: []string{"codex-local"}}
		o.seedChatToolBridge()
		runGroupAccessible(t, o.chatToolBridgeGroup(), "y")
		require.NoError(t, o.afterWizard())

		assert.Nil(t, o.skeletonConfig(nil).Chat.ToolBridge)
	})

	t.Run("hidden without a local CLI or without ai", func(t *testing.T) {
		t.Parallel()

		assert.True(t, (&SkeletonOptions{Features: []string{string(props.AiCmd)}, ChatProviders: []string{"claude"}}).chatToolBridgeHidden())
		assert.True(t, (&SkeletonOptions{ChatProviders: []string{"claude-local"}}).chatToolBridgeHidden())
		assert.False(t, (&SkeletonOptions{Features: []string{string(props.AiCmd)}, ChatProviders: []string{"claude-local"}}).chatToolBridgeHidden())
	})
}
