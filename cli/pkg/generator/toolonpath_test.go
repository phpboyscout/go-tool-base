package generator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A stubbed runner runs no real tool, so a test that stubs it never depends
// on what the developer's PATH holds (#194).
func TestToolOnPath_FollowsTheRunner(t *testing.T) {
	t.Parallel()

	const absent = "gtb-no-such-tool"

	realRunner := New(nil, &Config{})
	assert.False(t, realRunner.toolOnPath(absent), "the real runner looks on the real PATH")

	stubbed := New(nil, &Config{})
	stubbed.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	assert.True(t, stubbed.toolOnPath(absent), "a stubbed runner has every tool")

	stubbed.lookPath = func(string) (string, error) { return "", assert.AnError }
	assert.False(t, stubbed.toolOnPath("go"), "a lookup the test sets wins over the runner")
}
