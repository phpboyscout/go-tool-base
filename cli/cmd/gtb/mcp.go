package main

// Side-effect import: gives the shipped gtb binary the mcp feature.
// pkg/mcp declares it as a link kind and contributes the mcp command
// to the root, and it is the only framework package that reaches
// go/mcp and the MCP SDK.
//
// This file is the single on/off switch for MCP in the shipped gtb
// binary, as keychain.go is for the keychain. A build without it has
// no mcp command and carries neither module: a generated tool gets
// the same file as cmd/<name>/mcp.go from its manifest.
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
