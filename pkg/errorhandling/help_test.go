package errorhandling

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func TestHelp_SupportMessage(t *testing.T) {
	t.Parallel()

	providers := []struct {
		provider string
		help     HelpConfig
		channel  string
		want     string
	}{
		{
			provider: "Slack",
			help:     SlackHelp{Team: "Engineering", Channel: "#support"},
			channel:  "#support",
			want:     "For assistance, contact Engineering via Slack channel #support",
		},
		{
			provider: "Teams",
			help:     TeamsHelp{Team: "Engineering", Channel: "Support"},
			channel:  "Support",
			want:     "For assistance, contact Engineering via Microsoft Teams channel Support",
		},
	}

	for _, p := range providers {
		t.Run(p.provider, func(t *testing.T) {
			t.Parallel()

			tests := []struct {
				name    string
				help    HelpConfig
				want    string
				isEmpty bool
			}{
				{
					name:    "both fields set",
					help:    p.help,
					want:    p.want,
					isEmpty: false,
				},
				{
					name:    "missing team",
					help:    newHelp(p.provider, "", p.channel),
					isEmpty: true,
				},
				{
					name:    "missing channel",
					help:    newHelp(p.provider, "Engineering", ""),
					isEmpty: true,
				},
				{
					name:    "both empty",
					help:    newHelp(p.provider, "", ""),
					isEmpty: true,
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()

					msg := tt.help.SupportMessage()
					if tt.isEmpty {
						assert.Empty(t, msg)
					} else {
						assert.Equal(t, tt.want, msg)
					}
				})
			}
		})
	}
}

func newHelp(provider, team, channel string) HelpConfig {
	switch provider {
	case "Slack":
		return SlackHelp{Team: team, Channel: channel}
	case "Teams":
		return TeamsHelp{Team: team, Channel: channel}
	default:
		return nil
	}
}

func TestErrorHandler_HelpMessage_InOutput(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel))

	h := New(l, SlackHelp{Team: "Platform", Channel: "#alerts"})
	h.Error(errors.New("something went wrong"))

	assert.Contains(t, buf.String(), "For assistance, contact Platform via Slack channel #alerts")
}

func TestErrorHandler_NilHelp_NoHelpInOutput(t *testing.T) {
	var buf bytes.Buffer
	l := logger.NewCharm(&buf, logger.WithLevel(logger.InfoLevel))

	h := New(l, nil)
	h.Error(errors.New("something went wrong"))

	assert.NotContains(t, buf.String(), "For assistance")
}
