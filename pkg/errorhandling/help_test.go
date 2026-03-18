package errorhandling

import (
	"bytes"
	"errors"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
)

func TestSlackHelp_SupportMessage(t *testing.T) {
	tests := []struct {
		name    string
		help    SlackHelp
		want    string
		isEmpty bool
	}{
		{
			name:    "both fields set",
			help:    SlackHelp{Team: "Engineering", Channel: "#support"},
			want:    "For assistance, contact Engineering via Slack channel #support",
			isEmpty: false,
		},
		{
			name:    "missing team",
			help:    SlackHelp{Channel: "#support"},
			isEmpty: true,
		},
		{
			name:    "missing channel",
			help:    SlackHelp{Team: "Engineering"},
			isEmpty: true,
		},
		{
			name:    "both empty",
			help:    SlackHelp{},
			isEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.help.SupportMessage()
			if tt.isEmpty {
				assert.Empty(t, msg)
			} else {
				assert.Equal(t, tt.want, msg)
			}
		})
	}
}

func TestTeamsHelp_SupportMessage(t *testing.T) {
	tests := []struct {
		name    string
		help    TeamsHelp
		want    string
		isEmpty bool
	}{
		{
			name:    "both fields set",
			help:    TeamsHelp{Team: "Engineering", Channel: "Support"},
			want:    "For assistance, contact Engineering via Microsoft Teams channel Support",
			isEmpty: false,
		},
		{
			name:    "missing team",
			help:    TeamsHelp{Channel: "Support"},
			isEmpty: true,
		},
		{
			name:    "missing channel",
			help:    TeamsHelp{Team: "Engineering"},
			isEmpty: true,
		},
		{
			name:    "both empty",
			help:    TeamsHelp{},
			isEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.help.SupportMessage()
			if tt.isEmpty {
				assert.Empty(t, msg)
			} else {
				assert.Equal(t, tt.want, msg)
			}
		})
	}
}

func TestErrorHandler_HelpMessage_InOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{
		Level:     log.InfoLevel,
		Formatter: log.TextFormatter,
	})

	h := New(logger, SlackHelp{Team: "Platform", Channel: "#alerts"})
	h.Error(errors.New("something went wrong"))

	assert.Contains(t, buf.String(), "For assistance, contact Platform via Slack channel #alerts")
}

func TestErrorHandler_NilHelp_NoHelpInOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := log.NewWithOptions(&buf, log.Options{
		Level:     log.InfoLevel,
		Formatter: log.TextFormatter,
	})

	h := New(logger, nil)
	h.Error(errors.New("something went wrong"))

	assert.NotContains(t, buf.String(), "For assistance")
}
