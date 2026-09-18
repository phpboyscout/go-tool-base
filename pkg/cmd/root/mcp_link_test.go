package root_test

// The root registers the mcp command only when a main links pkg/mcp (spec
// 0202 D1). The integration tests here expect the tree a linked tool has,
// so the test binary links it the way cli/cmd/gtb/mcp.go does. This is a
// test-only import: pkg/cmd/root itself must not reach go/mcp, which the
// footprint test in pkg/props guards.
import _ "gitlab.com/phpboyscout/go-tool-base/pkg/mcp"
