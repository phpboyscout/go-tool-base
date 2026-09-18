package main

// Mirror cmd/gtb/mcp.go so the E2E test binary has the mcp feature it
// enables in main.go: without the link, enabling it is a construction
// error (spec 0199 OQ2), which is the right failure for a binary that
// claims a feature it did not link.
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
