# Commands

A set of consistent Cobra commands that can be used to implement common functionality for tools.

Includes:
- **`root`**: Application entry point, config loading, and service orchestration
- **`init`**: Interactive first-run configuration and credential setup
- **`version`**: Version display and update checking
- **`update`**: Self-updating from signed releases
- **`docs`**: Interactive TUI documentation browser
- **`mcp`**: AI agent integration (Model Context Protocol)
- **`doctor`**: Environment and configuration health checks
- **`config`**: Get/set/unset/edit/validate/migrate configuration
- **`changelog`**: Embedded version history
- **`telemetry`**: Opt-in pseudonymous usage analytics
- **`man`**: Roff man-page generation (opt-in)

Which commands are active is controlled by feature flags — see `props.SetFeatures`.

For detailed reference, usage, and feature-flag behaviour, see the **[CLI command reference](../../docs/reference/cli/index.md)**.
