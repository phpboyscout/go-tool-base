package testutil

import (
	"testing"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// EachConfigDir runs test once for each place a tool can keep its directory.
func EachConfigDir(t *testing.T, test func(t *testing.T, dir props.ConfigDir)) {
	t.Helper()

	for _, dir := range []props.ConfigDir{props.ConfigDirHome, props.ConfigDirXDG} {
		t.Run(string(dir), func(t *testing.T) { test(t, dir) })
	}
}
