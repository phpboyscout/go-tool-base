// Package root provides the reusable root Cobra command constructor that wires
// configuration loading, logging setup, update checks, and feature-flagged
// subcommand registration (version, update, init, doctor, config, telemetry,
// changelog, man, MCP, docs).
//
// The [NewCmdRoot] and [NewCmdRootWithConfig] functions build a root command
// whose PersistentPreRunE handles config merging (local files + embedded assets),
// log level/format configuration, and optional self-update prompting before any
// subcommand executes.
package root
