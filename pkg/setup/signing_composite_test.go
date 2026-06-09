package setup

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// fakeResolver is a test-only KeyResolver whose Resolve returns
// preconfigured (TrustSet, error) with an optional delay so
// concurrency tests can observe overlap.
type fakeResolver struct {
	name  string
	ts    *TrustSet
	err   error
	delay time.Duration
	calls int32
}

func (f *fakeResolver) Name() string { return f.name }

func (f *fakeResolver) Resolve(ctx context.Context) (*TrustSet, error) {
	atomic.AddInt32(&f.calls, 1)

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return f.ts, f.err
}

func TestCompositeResolver_Name(t *testing.T) {
	t.Parallel()

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "embedded"},
			&fakeResolver{name: "wkd:openpgpkey.example"},
		},
	}
	assert.Equal(t, "composite[embedded,wkd:openpgpkey.example]", c.Name())
}

func TestCompositeResolver_NoResolvers(t *testing.T) {
	t.Parallel()

	c := &CompositeResolver{}

	_, err := c.Resolve(context.Background())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrKeyResolverMismatch)
}

func TestCompositeResolver_AllAgree(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	ts1, err := LoadTrustSet(testEd25519.armoredPub, testRSA4096.armoredPub)
	require.NoError(t, err)
	ts2, err := LoadTrustSet(testEd25519.armoredPub, testRSA4096.armoredPub)
	require.NoError(t, err)

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "embedded", ts: ts1},
			&fakeResolver{name: "wkd", ts: ts2},
		},
		RequireAll: true,
	}

	got, err := c.Resolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ts1.Fingerprints(), got.Fingerprints())
}

func TestCompositeResolver_FingerprintMismatch(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	embedded, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)
	external, err := LoadTrustSet(testRSA4096.armoredPub)
	require.NoError(t, err)

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "embedded", ts: embedded},
			&fakeResolver{name: "wkd", ts: external},
		},
		RequireAll: true,
	}

	_, err = c.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverMismatch,
		"divergent fingerprint sets must surface as ErrKeyResolverMismatch")
}

func TestCompositeResolver_MismatchEvenWithRequireAllFalse(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	embedded, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)
	external, err := LoadTrustSet(testRSA4096.armoredPub)
	require.NoError(t, err)

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "embedded", ts: embedded},
			&fakeResolver{name: "wkd", ts: external},
		},
		RequireAll: false,
	}

	_, err = c.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverMismatch,
		"mismatch must abort regardless of RequireAll — tampering must never be silenced")
}

func TestCompositeResolver_OneFailsRequireAllTrue(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	embedded, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	boom := errors.New("wkd unreachable")
	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "embedded", ts: embedded},
			&fakeResolver{name: "wkd", err: boom},
		},
		RequireAll: true,
	}

	_, err = c.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverUnavailable,
		"child failure with RequireAll=true must abort")
}

func TestCompositeResolver_OneFailsRequireAllFalseLogsAndReturnsSuccess(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	embedded, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	buf := logger.NewBuffer()
	boom := errors.New("wkd unreachable")
	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "embedded", ts: embedded},
			&fakeResolver{name: "wkd", err: boom},
		},
		RequireAll: false,
		Logger:     buf,
	}

	got, err := c.Resolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, embedded.Fingerprints(), got.Fingerprints())

	// Buffer logger captures the warning.
	var sawWarn bool

	for _, e := range buf.Entries() {
		if e.Level == logger.WarnLevel && strings.Contains(e.Message, "composite resolver failed") {
			sawWarn = true

			break
		}
	}

	assert.True(t, sawWarn, "fail-open fallback must emit a Warn entry")
}

func TestCompositeResolver_AllFail(t *testing.T) {
	t.Parallel()

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "a", err: errors.New("boom-a")},
			&fakeResolver{name: "b", err: errors.New("boom-b")},
		},
		RequireAll: false,
	}

	_, err := c.Resolve(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverUnavailable)
}

func TestCompositeResolver_SingleResolverPasses(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	embedded, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "only", ts: embedded},
		},
		RequireAll: true,
	}

	got, err := c.Resolve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, embedded.Fingerprints(), got.Fingerprints())
}

func TestCompositeResolver_ConcurrentExecution(t *testing.T) {
	t.Parallel()
	mustInitTestSigningKeys(t)

	embedded, err := LoadTrustSet(testEd25519.armoredPub)
	require.NoError(t, err)

	// Two 200ms delays — serial would be ~400ms, concurrent ~200ms.
	const childDelay = 200 * time.Millisecond

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "a", ts: embedded, delay: childDelay},
			&fakeResolver{name: "b", ts: embedded, delay: childDelay},
		},
		RequireAll: true,
	}

	start := time.Now()
	_, err = c.Resolve(context.Background())
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 350*time.Millisecond,
		"children should run concurrently; serial would be ~400ms")
}

func TestCompositeResolver_ContextCanceled(t *testing.T) {
	t.Parallel()

	c := &CompositeResolver{
		Resolvers: []KeyResolver{
			&fakeResolver{name: "a", delay: 5 * time.Second},
			&fakeResolver{name: "b", delay: 5 * time.Second},
		},
		RequireAll: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := c.Resolve(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyResolverUnavailable,
		"context cancellation must surface as ErrKeyResolverUnavailable")
}
