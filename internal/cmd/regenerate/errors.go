package regenerate

import "gitlab.com/phpboyscout/go/errors"

var ErrInvalidOverwriteValue = errors.NewSentinel("gtb.regenerate.invalid_overwrite_value", "invalid --overwrite value: must be allow, deny, or ask")
