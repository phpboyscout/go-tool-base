package generator

import (
	"testing"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// emptyTestStore returns a store with no configuration — the single shared
// stand-in for the removed construction-only config containers, so the next
// config migration touches one place rather than every fixture.
func emptyTestStore(t *testing.T) *config.Store {
	t.Helper()

	return testutil.StoreFromYAML(t, "{}\n")
}
