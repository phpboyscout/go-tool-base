package browser_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/browser"
)

// fakeOpener records the URL it was invoked with (if any) and returns a
// caller-supplied error. Safe for concurrent use across t.Parallel tests.
type fakeOpener struct {
	mu       sync.Mutex
	invoked  bool
	url      string
	returned error
}

func (f *fakeOpener) Open(rawURL string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.invoked = true
	f.url = rawURL

	return f.returned
}

func (f *fakeOpener) Invoked() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.invoked
}

func (f *fakeOpener) URL() string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.url
}

func TestOpenURL_Accepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"plain https", "https://example.com"},
		{"https with port", "https://example.com:443"},
		{"https with path+query", "https://example.com/path?q=1&r=2"},
		{"http", "http://localhost:8080"},
		{"uppercase scheme", "HTTPS://example.com"},
		{"mixed-case scheme", "Https://example.com"},
		{"mailto basic", "mailto:user@example.com"},
		{"mailto with subject+body", "mailto:user@example.com?subject=Hi&body=there"},
		{"mailto uppercase", "MAILTO:user@example.com"},
		{"scheme-only", "https:"}, // valid per url.Parse; scheme matches
		{"max-length URL", "https://example.com/" + strings.Repeat("a", browser.MaxURLLength-len("https://example.com/"))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeOpener{}
			err := browser.OpenURL(context.Background(), tt.url, browser.WithOpener(opener.Open))
			require.NoError(t, err)
			assert.True(t, opener.Invoked(), "opener should have been invoked")
			assert.Equal(t, tt.url, opener.URL(), "opener should receive the exact URL")
		})
	}
}

func TestOpenURL_DisallowedScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"file", "file:///etc/passwd"},
		{"javascript", "javascript:alert(1)"},
		{"data", "data:text/html,<h1>hi</h1>"},
		{"vbscript", `vbscript:MsgBox("hi")`},
		{"custom proto", "myapp://callback"},
		{"ftp", "ftp://ftp.example.com/"},
		{"no scheme", "example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeOpener{}
			err := browser.OpenURL(context.Background(), tt.url, browser.WithOpener(opener.Open))
			require.ErrorIs(t, err, browser.ErrDisallowedScheme)
			assert.False(t, opener.Invoked(), "opener must not be invoked on disallowed scheme")
		})
	}
}

func TestOpenURL_InvalidURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"unparseable", "://bad"},
		{"null byte", "https://example.com/\x00path"},
		{"CR", "https://example.com/\rpath"},
		{"LF", "https://example.com/\npath"},
		{"CRLF", "https://example.com/\r\npath"},
		{"tab", "https://example.com/\tpath"},
		{"control char 0x01", "https://example.com/\x01"},
		{"control char 0x1F", "https://example.com/\x1f"},
		{"DEL 0x7F", "https://example.com/\x7f"},
		{"leading whitespace", "  https://example.com"},     // " " is 0x20, not a control char; but url.Parse rejects leading whitespace
		{"percent-encoded scheme", "h%74tps://example.com"}, // url.Parse rejects as invalid
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &fakeOpener{}
			err := browser.OpenURL(context.Background(), tt.url, browser.WithOpener(opener.Open))
			require.ErrorIs(t, err, browser.ErrInvalidURL)
			assert.False(t, opener.Invoked(), "opener must not be invoked on invalid URL")
		})
	}
}

func TestOpenURL_TooLong(t *testing.T) {
	t.Parallel()

	// Exactly MaxURLLength + 1 bytes.
	oversized := "https://example.com/" + strings.Repeat("a", browser.MaxURLLength-len("https://example.com/")+1)
	require.Greater(t, len(oversized), browser.MaxURLLength)

	opener := &fakeOpener{}
	err := browser.OpenURL(context.Background(), oversized, browser.WithOpener(opener.Open))
	require.ErrorIs(t, err, browser.ErrInvalidURL)
	assert.False(t, opener.Invoked())
}

func TestOpenURL_AtLengthLimit(t *testing.T) {
	t.Parallel()

	// Exactly MaxURLLength bytes — must succeed.
	atLimit := "https://example.com/" + strings.Repeat("a", browser.MaxURLLength-len("https://example.com/"))
	require.Len(t, atLimit, browser.MaxURLLength)

	opener := &fakeOpener{}
	err := browser.OpenURL(context.Background(), atLimit, browser.WithOpener(opener.Open))
	require.NoError(t, err)
	assert.True(t, opener.Invoked())
}

func TestOpenURL_ContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opener := &fakeOpener{}
	err := browser.OpenURL(ctx, "https://example.com", browser.WithOpener(opener.Open))
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, opener.Invoked(), "opener must not be invoked after context cancel")
}

func TestOpenURL_ContextDeadlineExceeded(t *testing.T) {
	t.Parallel()

	// An already-expired deadline — triggers the ctx.Err() check.
	ctx, cancel := context.WithDeadline(context.Background(),
		// Past deadline: now - 1s
		alreadyPastTime())
	defer cancel()

	opener := &fakeOpener{}
	err := browser.OpenURL(ctx, "https://example.com", browser.WithOpener(opener.Open))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, opener.Invoked())
}

func TestOpenURL_OpenerError(t *testing.T) {
	t.Parallel()

	openerErr := errors.New("fake opener failed")
	opener := &fakeOpener{returned: openerErr}

	err := browser.OpenURL(context.Background(), "https://example.com", browser.WithOpener(opener.Open))
	require.ErrorIs(t, err, openerErr)
	assert.True(t, opener.Invoked())
}

func TestOpenURL_NilOpenerOption(t *testing.T) {
	t.Parallel()

	// Passing WithOpener(nil) should keep the default opener.
	// We can't actually verify the default is invoked without launching a
	// real browser, so instead we pass a nil option followed by a real
	// fake and assert the fake wins (last-option-wins semantics).
	opener := &fakeOpener{}
	err := browser.OpenURL(context.Background(), "https://example.com",
		browser.WithOpener(nil),
		browser.WithOpener(opener.Open),
	)
	require.NoError(t, err)
	assert.True(t, opener.Invoked())
}

func TestOpenURL_MultipleOpenersLastWins(t *testing.T) {
	t.Parallel()

	first := &fakeOpener{}
	last := &fakeOpener{}

	err := browser.OpenURL(context.Background(), "https://example.com",
		browser.WithOpener(first.Open),
		browser.WithOpener(last.Open),
	)
	require.NoError(t, err)
	assert.False(t, first.Invoked())
	assert.True(t, last.Invoked())
}

func TestValidateBeforeOpen(t *testing.T) {
	t.Parallel()

	// Disallowed scheme with a cancelled context — validation should fail
	// with ErrDisallowedScheme, not with context.Canceled, because scheme
	// is checked before context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	opener := &fakeOpener{}
	err := browser.OpenURL(ctx, "file:///etc/passwd", browser.WithOpener(opener.Open))
	require.ErrorIs(t, err, browser.ErrDisallowedScheme)
	require.NotErrorIs(t, err, context.Canceled,
		"disallowed-scheme error must take precedence over context cancellation")
	assert.False(t, opener.Invoked())
}

// alreadyPastTime returns a time in the past for deadline testing.
func alreadyPastTime() (t time.Time) {
	return time.Unix(0, 0)
}
