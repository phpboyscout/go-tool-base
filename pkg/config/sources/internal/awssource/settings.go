package awssource

import (
	"time"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// PollInterval reads a slot's optional poll_interval.
func PollInterval(settings config.Reader) (time.Duration, error) {
	raw := settings.GetString("poll_interval")
	if raw == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(raw)

	return d, errors.Wrap(err, "poll_interval")
}

// Settings are the initialiser questions every AWS kind shares, after its
// own.
func Settings() []setup.SourceSetting {
	return []setup.SourceSetting{
		{Key: "region", Title: "AWS region", Description: "Leave empty to use the region the AWS chain resolves"},
		{Key: "profile", Title: "AWS profile", Description: "Leave empty to use the default credential chain"},
	}
}
