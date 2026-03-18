package errorhandling

import "fmt"

// HelpConfig is the interface for providing contextual support information when errors occur.
// Implementations return a human-readable message directing users to a support channel.
// Returning an empty string suppresses the help output.
type HelpConfig interface {
	SupportMessage() string
}

// SlackHelp provides Slack channel contact details as a support message.
type SlackHelp struct {
	Team    string `json:"team" yaml:"team"`
	Channel string `json:"channel" yaml:"channel"`
}

func (s SlackHelp) SupportMessage() string {
	if s.Team == "" || s.Channel == "" {
		return ""
	}

	return fmt.Sprintf("For assistance, contact %s via Slack channel %s", s.Team, s.Channel)
}

// TeamsHelp provides Microsoft Teams channel contact details as a support message.
type TeamsHelp struct {
	Team    string `json:"team" yaml:"team"`
	Channel string `json:"channel" yaml:"channel"`
}

func (t TeamsHelp) SupportMessage() string {
	if t.Team == "" || t.Channel == "" {
		return ""
	}

	return fmt.Sprintf("For assistance, contact %s via Microsoft Teams channel %s", t.Team, t.Channel)
}
