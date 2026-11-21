package generator

import (
	"go/token"
	"strings"
)

// commandPackageName is the Go package a command's code lives in. A name that
// is a Go keyword takes a "cmd" suffix, since `package import` cannot compile;
// the command's name, directory and constructor keep the name (spec 0216).
func commandPackageName(name string) string {
	pkg := strings.ReplaceAll(name, "-", "_")
	if token.IsKeyword(pkg) {
		return pkg + "cmd"
	}

	return pkg
}
