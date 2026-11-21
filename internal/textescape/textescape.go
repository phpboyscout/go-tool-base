// Package textescape makes untrusted text safe to print to a terminal.
package textescape

import (
	"fmt"
	"strings"
)

// lastBMPRune is the highest rune four hex digits can write.
const lastBMPRune = 0xFFFF

// Unless writes every rune keep rejects as \uXXXX or \UXXXXXXXX, so a
// newline cannot forge a line of output and a host cannot be dressed as
// another. A backslash is doubled, so text that reads as an escape is not one.
func Unless(keep func(rune) bool, s string) string {
	var out strings.Builder

	for _, r := range s {
		switch {
		case r == '\\':
			out.WriteString(`\\`)
		case keep(r):
			out.WriteRune(r)
		case r > lastBMPRune:
			fmt.Fprintf(&out, `\U%08x`, r)
		default:
			fmt.Fprintf(&out, `\u%04x`, r)
		}
	}

	return out.String()
}
