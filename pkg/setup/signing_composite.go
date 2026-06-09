package setup

import (
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// CompositeResolver wraps an ordered list of KeyResolvers and requires
// them to agree on the set of key fingerprints. It is the production
// default for tools that publish both an embedded key and an external
// (WKD) key: trust is only granted when both sources match, so an
// attacker must compromise both within the same window.
//
// RequireAll controls the failure mode when a child resolver returns
// a non-mismatch error (network failure, weak key, malformed data):
//
//   - true  — any child failure aborts with ErrKeyResolverUnavailable
//     wrapping the first child's error. The fail-closed posture.
//   - false — child failures are logged at Warn and the composite
//     returns the successful children's trust set, as long as at
//     least one resolver succeeded. The fail-open posture, suitable
//     for tools that tolerate WKD outages.
//
// Fingerprint disagreement among successful resolvers always aborts
// with ErrKeyResolverMismatch, regardless of RequireAll: a mismatch
// means at least one trust anchor has been tampered with.
type CompositeResolver struct {
	Resolvers  []KeyResolver
	RequireAll bool
	Logger     logger.Logger
}

// resolverResult bundles a child resolver's outcome.
type resolverResult struct {
	name string
	ts   *TrustSet
	err  error
}

// Name returns "composite[<child1>,<child2>,...]" for diagnostics.
func (c *CompositeResolver) Name() string {
	names := make([]string, len(c.Resolvers))
	for i, r := range c.Resolvers {
		names[i] = r.Name()
	}

	return "composite[" + strings.Join(names, ",") + "]"
}

// Resolve runs each child resolver concurrently with the supplied
// context and applies the RequireAll / fingerprint-agreement policy.
// Returns ErrKeyResolverUnavailable when no usable trust set survives,
// or ErrKeyResolverMismatch when multiple resolvers succeed with
// divergent fingerprint sets.
func (c *CompositeResolver) Resolve(ctx context.Context) (*TrustSet, error) {
	if len(c.Resolvers) == 0 {
		return nil, errors.New("composite: no resolvers configured")
	}

	results := c.runChildren(ctx)
	successes, firstErr := c.partition(results)

	if c.RequireAll && firstErr != nil {
		return nil, errors.Wrap(ErrKeyResolverUnavailable, firstErr.Error())
	}

	if len(successes) == 0 {
		return nil, errors.Wrap(ErrKeyResolverUnavailable, "composite: no resolver succeeded")
	}

	if err := checkAgreement(successes); err != nil {
		return nil, err
	}

	return successes[0].ts, nil
}

// runChildren invokes every configured resolver concurrently and
// returns their results in registration order.
func (c *CompositeResolver) runChildren(ctx context.Context) []resolverResult {
	results := make([]resolverResult, len(c.Resolvers))

	var wg sync.WaitGroup
	for i, r := range c.Resolvers {
		wg.Add(1)

		go func(i int, r KeyResolver) {
			defer wg.Done()

			ts, err := r.Resolve(ctx)
			results[i] = resolverResult{name: r.Name(), ts: ts, err: err}
		}(i, r)
	}

	wg.Wait()

	return results
}

// partition splits resolver results into successes and the first
// failure (in registration order). Failed resolvers are warned at
// Warn level when RequireAll is false and a logger is configured.
func (c *CompositeResolver) partition(results []resolverResult) ([]resolverResult, error) {
	var (
		successes []resolverResult
		firstErr  error
	)

	for _, res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = errors.Wrapf(res.err, "composite: resolver %q", res.name)
			}

			if c.Logger != nil && !c.RequireAll {
				c.Logger.Warn("composite resolver failed (RequireAll=false, continuing)",
					"resolver", res.name, "err", res.err.Error())
			}

			continue
		}

		successes = append(successes, res)
	}

	return successes, firstErr
}

// minResolversForAgreement is the minimum number of successful
// resolvers needed before a cross-check is meaningful — a single
// resolver has nothing to disagree with.
const minResolversForAgreement = 2

// checkAgreement returns ErrKeyResolverMismatch when the supplied
// successful resolvers do not all return the same set of fingerprints.
func checkAgreement(successes []resolverResult) error {
	if len(successes) < minResolversForAgreement {
		return nil
	}

	reference := successes[0].ts.Fingerprints()
	for _, s := range successes[1:] {
		if !reflect.DeepEqual(reference, s.ts.Fingerprints()) {
			return errors.Wrapf(ErrKeyResolverMismatch,
				"composite: %q vs %q fingerprint sets diverge",
				successes[0].name, s.name)
		}
	}

	return nil
}
