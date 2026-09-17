// Package gomod seeds a scaffold's go.mod with the direct requirements the
// generator's own import lines imply, and nothing more (spec 0200).
//
// go mod tidy stays the owner of the file: the seed adds a missing require
// line at a known version, drops a line of its own whose import has gone,
// raises a line below a floor GTB declares, and leaves everything else,
// including the version of any other present line, exactly as it found it.
// It is pure over bytes so it is tested on literal go.mod text.
package gomod
