package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go/signing"
)

// TestNoAWSSmoke verifies the regulated-build slice: gtb signs nothing
// itself, so nothing it links registers a signing backend, and the KMS
// backend cannot ride in on one.
//
// The link-time arm (no github.com/aws/* in the dependency closure) is
// kept out of go test, because it needs the build toolchain.
func TestNoAWSSmoke(t *testing.T) {
	t.Parallel()

	assert.Empty(t, signing.Names(), "gtb's root must register no signing backend; one would bring its SDK with it")
}
