package credentialposture_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// TestReportWhere pins spec 0196 D12: a reporting surface passes one
// predicate over the whole descriptor, so doctor can ask "is the feature
// enabled and is the provider this credential serves actually linked".
func TestReportWhere(t *testing.T) {
	credentialposture.Register(credentialposture.Descriptor{Owner: "where:a", Label: "A", Feature: "ai", Providers: []string{"pa"}, EnvKey: "where.a.env"})
	credentialposture.Register(credentialposture.Descriptor{Owner: "where:b", Label: "B", Feature: "ai", Providers: []string{"pb", "pb2"}, EnvKey: "where.b.env"})
	credentialposture.Register(credentialposture.Descriptor{Owner: "where:c", Label: "C", Feature: "forge", EnvKey: "where.c.env"})

	cfg := fakeReader{"where.a.env": "X", "where.b.env": "X", "where.c.env": "X"}
	t.Setenv("X", "v")

	linked := map[string]bool{"pb2": true}
	results := credentialposture.ReportWhere(context.Background(), cfg, func(d credentialposture.Descriptor) bool {
		if d.Feature != "ai" {
			return false
		}

		for _, p := range d.Providers {
			if linked[p] {
				return true
			}
		}

		return len(d.Providers) == 0
	})

	labels := make([]string, 0, len(results))
	for _, r := range results {
		labels = append(labels, r.Posture.Label)
	}

	require.Contains(t, labels, "B", "a descriptor one of whose providers is linked is reported")
	assert.NotContains(t, labels, "A", "a descriptor whose provider is not linked is not")
	assert.NotContains(t, labels, "C", "another feature's descriptor is not")
}
