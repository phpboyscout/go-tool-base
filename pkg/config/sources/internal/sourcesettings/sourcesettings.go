// Package sourcesettings reads the settings several source kinds share.
package sourcesettings

import (
	"time"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
)

// PollInterval reads a slot's optional poll_interval; zero when unset.
func PollInterval(settings config.Reader) (time.Duration, error) {
	raw := settings.GetString("poll_interval")
	if raw == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(raw)

	return d, errors.Wrap(err, "poll_interval")
}
