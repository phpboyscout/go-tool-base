package regenerate

import "github.com/cockroachdb/errors"

var ErrInvalidOverwriteValue = errors.New("invalid --overwrite value: must be allow, deny, or ask")
