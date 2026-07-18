package props

import "fmt"

// SlackHelp provides Slack channel contact details as a support message.
// It implements the errorhandling.HelpConfig interface, which the
// go/errorhandling module defines but deliberately ships no implementations
// for — where a team's support channel lives is a framework/tool concern, not
// something an error-reporting library should have an opinion about.
//
// Assign it to [Tool.Help]; the scaffolded root command does this for tools
// generated with `--help-type slack`.
type SlackHelp struct {
	Team    string `json:"team" yaml:"team"`
	Channel string `json:"channel" yaml:"channel"`
}

// SupportMessage renders the Slack contact line, or an empty string when the
// channel is not fully configured (an empty message suppresses help output).
func (s SlackHelp) SupportMessage() string {
	if s.Team == "" || s.Channel == "" {
		return ""
	}

	return fmt.Sprintf("For assistance, contact %s via Slack channel %s", s.Team, s.Channel)
}

// TeamsHelp provides Microsoft Teams channel contact details as a support
// message. See [SlackHelp] for the rationale behind these living in GTB rather
// than in the errorhandling module.
type TeamsHelp struct {
	Team    string `json:"team" yaml:"team"`
	Channel string `json:"channel" yaml:"channel"`
}

// SupportMessage renders the Teams contact line, or an empty string when the
// channel is not fully configured (an empty message suppresses help output).
func (t TeamsHelp) SupportMessage() string {
	if t.Team == "" || t.Channel == "" {
		return ""
	}

	return fmt.Sprintf("For assistance, contact %s via Microsoft Teams channel %s", t.Team, t.Channel)
}
