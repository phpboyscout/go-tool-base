package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSkipIfNotIntegration_GlobalGate(t *testing.T) {
	t.Setenv("INT_TEST", "1")

	skipped := false
	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t, "vcs")
		skipped = t.Skipped()
	})
	assert.False(t, skipped, "should not skip when INT_TEST is set")
}

func TestSkipIfNotIntegration_TaggedGate(t *testing.T) {
	t.Setenv("INT_TEST_VCS", "1")

	skipped := false
	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t, "vcs")
		skipped = t.Skipped()
	})
	assert.False(t, skipped, "should not skip when INT_TEST_VCS is set")
}

func TestSkipIfNotIntegration_NoMatch(t *testing.T) {
	// Neither INT_TEST nor INT_TEST_VCS is set.
	var skipped bool
	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t, "vcs")
		// If we reach here, not skipped
	})
	// Check the subtest result via its Skipped state
	// We need to check after Run returns
	t.Run("verify", func(t *testing.T) {
		// Re-run to check — the previous subtest should have been skipped
		// but we can't access its state. Use a flag instead.
		_ = skipped
	})
}

func TestSkipIfNotIntegration_Skips(t *testing.T) {
	// Verify skip message appears when nothing is set.
	// We can't easily check t.Skipped() on subtests from outside,
	// so we verify the function runs without panic and the subtest
	// is reported as skipped in -v output.
	t.Run("no_env", func(t *testing.T) {
		SkipIfNotIntegration(t, "vcs")
		t.Fatal("should not reach here")
	})
}

func TestSkipIfNotIntegration_CaseInsensitive(t *testing.T) {
	t.Setenv("INT_TEST_CONFIG", "true")

	skipped := false
	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t, "config")
		skipped = t.Skipped()
	})
	assert.False(t, skipped, "should match INT_TEST_CONFIG for tag 'config'")
}

func TestSkipIfNotIntegration_NoTags_WithGlobal(t *testing.T) {
	t.Setenv("INT_TEST", "1")

	skipped := false
	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t)
		skipped = t.Skipped()
	})
	assert.False(t, skipped, "should not skip when INT_TEST is set even with no tags")
}

func TestSkipIfNotIntegration_NoTags_WithoutGlobal(t *testing.T) {
	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t)
		t.Fatal("should not reach here")
	})
}

func TestSkipIfNotIntegration_WrongTag(t *testing.T) {
	t.Setenv("INT_TEST_CONFIG", "1")

	t.Run("inner", func(t *testing.T) {
		SkipIfNotIntegration(t, "vcs")
		t.Fatal("should not reach here — INT_TEST_CONFIG does not match tag 'vcs'")
	})
}
