package docs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultManual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty falls back to bare Manual", "", "Manual"},
		{"lowercases title-cased", "gtb", "Gtb Manual"},
		{"preserves remaining runes", "myTool", "MyTool Manual"},
		{"handles single rune", "x", "X Manual"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, defaultManual(tt.in))
		})
	}
}
