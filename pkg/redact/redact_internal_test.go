package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestKeepPrefix is a white-box test of the masking helper. The previously
// uncovered branch is the fall-through that masks the whole value when the
// requested prefix length is out of range (zero/negative, or >= the value's
// length) — important so a boundary-length token is never partially leaked.
func TestKeepPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		match string
		keep  int
		want  string
	}{
		{"keeps prefix when keep is within bounds", "sk-ant-secret", 3, "sk-***"},
		{"keep of zero masks entirely", "sk-ant-secret", 0, "***"},
		{"negative keep masks entirely", "sk-ant-secret", -1, "***"},
		{"keep equal to length masks entirely", "short", 5, "***"},
		{"keep greater than length masks entirely", "abc", 10, "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, keepPrefix(tt.match, tt.keep))
		})
	}
}
