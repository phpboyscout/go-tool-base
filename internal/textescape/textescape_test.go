package textescape_test

import (
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/textescape"
)

func TestUnless(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{in: "mise", want: "mise"},
		{in: "a\nb", want: `a\u000ab`},
		{in: "\x1b[31mred", want: `\u001b[31mred`},
		{in: `a\u000ab`, want: `a\\u000ab`},
		{in: "\U000E0001", want: `\U000e0001`},
	} {
		assert.Equal(t, tt.want, textescape.Unless(unicode.IsPrint, tt.in), "%q", tt.in)
	}
}
