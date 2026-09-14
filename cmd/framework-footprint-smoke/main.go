// Command framework-footprint-smoke is a compile-time fixture: a tool built on
// the framework that imports pkg/cmd/root and nothing else. Its test builds it
// and asserts that no chat-provider or forge-adapter module reached the binary,
// which is what keeps registration out of pkg/ (spec 0194 D3) after the
// spike's measurement is forgotten: 81 MB with the adapters in pkg/, 34 MB
// without, for a root command that does nothing.
package main

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func main() {
	root.Execute(root.NewCmdRoot(&props.Props{}), &props.Props{})
}
