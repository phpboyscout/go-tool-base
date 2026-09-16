package credentialposture_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// TestReportFor pins spec 0196 D12 over spec 0199 OQ5: the Set gates by
// feature, and the predicate narrows to the credentials whose provider is
// linked, so doctor asks "is the feature enabled" of the Set and "is the
// provider linked" of the descriptor.
func TestReportFor(t *testing.T) {
	t.Setenv("X", "v")

	r := features.NewRegistry()
	require.NoError(t, r.Declare(testFeature{id: "ai"}))
	require.NoError(t, r.Declare(testFeature{id: "forge"}))

	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "where:a", Label: "A", Feature: "ai", Providers: []string{"pa"}, EnvKey: "where.a.env"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "where:b", Label: "B", Feature: "ai", Providers: []string{"pb", "pb2"}, EnvKey: "where.b.env"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "where:c", Label: "C", Feature: "forge", EnvKey: "where.c.env"})

	set, err := features.Resolve(r.Snapshot(), []features.State{{ID: "ai", Enabled: true}})
	require.NoError(t, err)

	cfg := fakeReader{"where.a.env": "X", "where.b.env": "X", "where.c.env": "X"}

	linked := map[string]bool{"pb2": true}
	results := credentialposture.ReportFor(context.Background(), cfg, set, func(d credentialposture.Descriptor) bool {
		for _, p := range d.Providers {
			if linked[p] {
				return true
			}
		}

		return len(d.Providers) == 0
	})

	labels := make([]string, 0, len(results))
	for _, res := range results {
		labels = append(labels, res.Posture.Label)
	}

	require.Contains(t, labels, "B", "a descriptor one of whose providers is linked is reported")
	assert.NotContains(t, labels, "A", "a descriptor whose provider is not linked is not")
	assert.NotContains(t, labels, "C", "a disabled feature's descriptor is not")
}
