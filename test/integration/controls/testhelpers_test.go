package controls_test

import (
	"bytes"
	"sync"
)

// syncBuffer is a thread-safe bytes.Buffer for capturing controller log output
// in the integration tests (the controller writes from multiple goroutines).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}
