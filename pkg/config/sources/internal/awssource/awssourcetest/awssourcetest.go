// Package awssourcetest isolates AWS source tests from the developer's environment.
package awssourcetest

import "testing"

// Isolate gives a test static AWS credentials and a HOME of its own, so the
// SDK's chain resolves without reading the developer's ~/.aws or reaching
// instance metadata. It sets environment variables, so its caller cannot run
// in parallel.
func Isolate(t testing.TB) {
	t.Helper()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDTEST")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "eu-west-2")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
}
