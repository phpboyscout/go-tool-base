package main

// Mirror cmd/gtb/signing.go so the e2e test binary registers the same
// signing backends as the shipped product. Without these imports the
// `gtb keys mint --backend …` scenarios would all fail with
// `ErrUnknownBackend` because the registry would be empty.
//
// The cmd/gtb-no-aws-smoke binary deliberately omits the kms import to
// prove the AWS SDK can be left out of regulated builds; e2e is the
// opposite — full feature surface, all backends linked in.
import (
	_ "gitlab.com/phpboyscout/go/signing-aws-kms"
	_ "gitlab.com/phpboyscout/go/signing/local"
)
