package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Both implementations satisfy the module's extension point.
var (
	_ errorhandling.HelpConfig = props.SlackHelp{}
	_ errorhandling.HelpConfig = props.TeamsHelp{}
)

func TestHelp_SupportMessage(t *testing.T) {
	t.Parallel()

	providers := []struct {
		provider string
		help     errorhandling.HelpConfig
		want     string
		newHelp  func(team, channel string) errorhandling.HelpConfig
	}{
		{
			provider: "Slack",
			help:     props.SlackHelp{Team: "Engineering", Channel: "#support"},
			want:     "For assistance, contact Engineering via Slack channel #support",
			newHelp: func(team, channel string) errorhandling.HelpConfig {
				return props.SlackHelp{Team: team, Channel: channel}
			},
		},
		{
			provider: "Teams",
			help:     props.TeamsHelp{Team: "Engineering", Channel: "Support"},
			want:     "For assistance, contact Engineering via Microsoft Teams channel Support",
			newHelp: func(team, channel string) errorhandling.HelpConfig {
				return props.TeamsHelp{Team: team, Channel: channel}
			},
		},
	}

	for _, p := range providers {
		t.Run(p.provider, func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name    string
				help    errorhandling.HelpConfig
				want    string
				isEmpty bool
			}{
				{name: "both fields set", help: p.help, want: p.want},
				{name: "missing team", help: p.newHelp("", "#support"), isEmpty: true},
				{name: "missing channel", help: p.newHelp("Engineering", ""), isEmpty: true},
				{name: "both empty", help: p.newHelp("", ""), isEmpty: true},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					msg := tt.help.SupportMessage()
					if tt.isEmpty {
						assert.Empty(t, msg, "an unconfigured channel must suppress help output")
					} else {
						assert.Equal(t, tt.want, msg)
					}
				})
			}
		})
	}
}
