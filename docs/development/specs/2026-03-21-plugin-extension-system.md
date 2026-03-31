---
title: "Plugin Extension System Specification"
description: "Script-based command plugin system allowing users to extend CLI tools with custom commands discovered from a plugins directory."
date: 2026-03-21
status: REJECTED
tags:
  - specification
  - plugins
  - extensibility
  - feature
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-6)
    role: AI drafting assistant
---

# Plugin Extension System Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-6) *(AI drafting assistant)*

Date
:   21 March 2026

Status
:   DRAFT

---

## Rejection Rationale

This spec was rejected on 31 March 2026. The plugin system solves a problem that doesn't naturally exist in GTB's library-first architecture:

- **GTB tools are compiled Go binaries** where the tool author controls command registration. Unlike `kubectl` (a universal binary that can't be recompiled by end users), GTB tools are purpose-built by tool authors who have full source access.
- **The `gtb generate command` workflow** already converts scripts into native Cobra commands with full type safety, middleware integration, and telemetry support — without the complexity of subprocess management, manifest parsing, or security surface area.
- **Adding commands is already trivial** via the generator. A script-based plugin system would be a parallel mechanism that adds complexity without clear value over the existing approach.

The feature may be reconsidered if a concrete consumer with a real need emerges that cannot be served by the generator's script-to-command conversion.

---

## Overview (Original)

Users cannot extend GTB-based tools with custom commands without forking the project. A plugin system allows users to drop scripts or binaries into a well-known directory and have them automatically discovered and registered as subcommands. This follows the pattern established by tools like `git` (any `git-foo` on PATH becomes `git foo`), `kubectl` plugins, and Homebrew taps.

---

## Design Decisions

**Script-based, not Go plugins**: Go's `plugin` package has severe limitations (Linux/macOS only, exact Go version match, no unloading). Script-based plugins work with any language, are simpler to author, and match user expectations from kubectl/git patterns.

**Manifest file for metadata**: Each plugin provides a `plugin.yaml` manifest declaring its name, description, version, and argument schema. Without a manifest, the plugin is still discovered but gets minimal help text.

**Subprocess execution**: Plugins run as subprocesses. GTB passes arguments via command-line args and environment variables (config values, tool metadata). Plugin stdout/stderr are captured and relayed to the user.

**Feature flag gated**: Plugin loading is controlled by a `PluginsCmd` feature flag, disabled by default until the system is stable.

**Security boundary**: Plugins execute with the user's permissions. GTB does not sandbox them. A warning is logged on first plugin discovery. Plugins from untrusted sources are the user's responsibility.

---

## Public API Changes

### New Feature Flag

```go
// In pkg/props/tool.go
const PluginsCmd FeatureCmd = "plugins"
```

### New Package: `pkg/plugins`

```go
// Plugin represents a discovered plugin.
type Plugin struct {
    Name        string
    Description string
    Version     string
    Path        string   // absolute path to the executable
    Args        []Arg    // declared arguments from manifest
}

// Arg describes a plugin argument.
type Arg struct {
    Name        string
    Description string
    Required    bool
    Default     string
}

// Manifest is the plugin.yaml schema.
type Manifest struct {
    Name        string `yaml:"name"`
    Description string `yaml:"description"`
    Version     string `yaml:"version"`
    Args        []Arg  `yaml:"args"`
}

// Discoverer finds and loads plugins from the plugins directory.
type Discoverer interface {
    Discover() ([]Plugin, error)
}

// Executor runs a plugin as a subprocess.
type Executor interface {
    Execute(ctx context.Context, plugin Plugin, args []string) error
}
```

---

## Internal Implementation

### Plugin Directory Structure

```
~/.toolname/plugins/
├── my-plugin/
│   ├── plugin.yaml       # manifest (optional but recommended)
│   └── run.sh            # executable (must be chmod +x)
├── another-plugin/
│   ├── plugin.yaml
│   └── main.py
└── simple-script         # single-file plugin (no manifest)
```

### Plugin Discovery

```go
type fsDiscoverer struct {
    fs         afero.Fs
    pluginsDir string
}

func (d *fsDiscoverer) Discover() ([]Plugin, error) {
    entries, err := afero.ReadDir(d.fs, d.pluginsDir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // no plugins dir is fine
        }
        return nil, errors.Wrap(err, "reading plugins directory")
    }

    var plugins []Plugin
    for _, entry := range entries {
        plugin, err := d.loadPlugin(entry)
        if err != nil {
            // Log warning but continue — one bad plugin shouldn't break others
            continue
        }
        plugins = append(plugins, plugin)
    }
    return plugins, nil
}

func (d *fsDiscoverer) loadPlugin(entry os.FileInfo) (Plugin, error) {
    pluginPath := filepath.Join(d.pluginsDir, entry.Name())

    if entry.IsDir() {
        return d.loadDirectoryPlugin(pluginPath, entry.Name())
    }
    return d.loadFilePlugin(pluginPath, entry.Name())
}
```

### Manifest Loading

```go
func (d *fsDiscoverer) loadManifest(dir string) (*Manifest, error) {
    manifestPath := filepath.Join(dir, "plugin.yaml")
    data, err := afero.ReadFile(d.fs, manifestPath)
    if err != nil {
        return nil, err // manifest is optional
    }

    var m Manifest
    if err := yaml.Unmarshal(data, &m); err != nil {
        return nil, errors.Wrap(err, "parsing plugin manifest")
    }
    return &m, nil
}
```

### Plugin Execution

```go
type subprocessExecutor struct {
    logger *slog.Logger
    env    []string
}

func (e *subprocessExecutor) Execute(ctx context.Context, plugin Plugin, args []string) error {
    cmd := exec.CommandContext(ctx, plugin.Path, args...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    cmd.Env = append(os.Environ(), e.env...)

    e.logger.Info("executing plugin", "name", plugin.Name, "path", plugin.Path)

    if err := cmd.Run(); err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            return errors.Newf("plugin %q exited with code %d", plugin.Name, exitErr.ExitCode())
        }
        return errors.Wrapf(err, "executing plugin %q", plugin.Name)
    }
    return nil
}
```

### Environment Variables Passed to Plugins

| Variable | Description |
|----------|-------------|
| `GTB_TOOL_NAME` | Name of the parent tool |
| `GTB_TOOL_VERSION` | Version of the parent tool |
| `GTB_CONFIG_DIR` | Path to the tool's config directory |
| `GTB_PLUGINS_DIR` | Path to the plugins directory |
| `GTB_VERBOSE` | "true" if verbose mode is active |

### Cobra Command Registration

```go
func RegisterPlugins(rootCmd *cobra.Command, props *p.Props) {
    if props.Tool.IsDisabled(p.PluginsCmd) {
        return
    }

    discoverer := NewFSDiscoverer(props.FS, pluginsDir(props))
    plugins, err := discoverer.Discover()
    if err != nil {
        props.Logger.Warn("failed to discover plugins", "error", err)
        return
    }

    executor := NewSubprocessExecutor(props.Logger, buildPluginEnv(props))

    for _, plugin := range plugins {
        cmd := &cobra.Command{
            Use:   plugin.Name,
            Short: plugin.Description,
            RunE: func(cmd *cobra.Command, args []string) error {
                return executor.Execute(cmd.Context(), plugin, args)
            },
        }
        rootCmd.AddCommand(cmd)
    }
}
```

### Validation

```go
func (d *fsDiscoverer) validatePlugin(plugin Plugin) error {
    // Check executable exists
    info, err := d.fs.Stat(plugin.Path)
    if err != nil {
        return errors.Wrap(err, "plugin executable not found")
    }

    // Check executable permission (Unix only)
    if info.Mode()&0111 == 0 {
        return errors.Newf("plugin %q is not executable: %s", plugin.Name, plugin.Path)
    }

    // Check name doesn't conflict with built-in commands
    // (handled at registration time by Cobra)

    return nil
}
```

---

## Project Structure

```
pkg/plugins/
├── plugins.go         ← NEW: Plugin, Manifest, Discoverer, Executor types
├── discoverer.go      ← NEW: fsDiscoverer implementation
├── executor.go        ← NEW: subprocessExecutor implementation
├── plugins_test.go    ← NEW: discovery and execution tests
pkg/props/
├── tool.go            ← MODIFIED: add PluginsCmd feature flag
pkg/cmd/root/
├── root.go            ← MODIFIED: call RegisterPlugins
```

---

## Testing Strategy

### Unit Tests

| Test | Scenario |
|------|----------|
| `TestDiscover_EmptyDir` | No plugins directory → empty list, no error |
| `TestDiscover_SinglePlugin` | One plugin with manifest → correctly loaded |
| `TestDiscover_NoManifest` | Plugin without manifest → discovered with name from directory |
| `TestDiscover_InvalidManifest` | Malformed YAML → plugin skipped with warning |
| `TestDiscover_NotExecutable` | Non-executable file → plugin skipped with warning |
| `TestDiscover_MultiplePlugins` | Three plugins → all discovered |
| `TestExecute_Success` | Plugin script exits 0 → no error |
| `TestExecute_NonZeroExit` | Plugin exits with code 1 → error with exit code |
| `TestExecute_ContextCancelled` | Context cancelled → plugin killed |
| `TestExecute_Environment` | Plugin receives expected env vars |
| `TestRegisterPlugins_Disabled` | Feature flag off → no plugins registered |
| `TestRegisterPlugins_ConflictingName` | Plugin name matches built-in → Cobra handles conflict |

### Test Plugin Script

```go
func createTestPlugin(t *testing.T, fs afero.Fs, dir, name, content string) {
    t.Helper()
    pluginDir := filepath.Join(dir, name)
    fs.MkdirAll(pluginDir, 0755)
    afero.WriteFile(fs, filepath.Join(pluginDir, "plugin.yaml"), []byte(fmt.Sprintf(
        "name: %s\ndescription: test plugin\nversion: 1.0.0\n", name)), 0644)
    scriptPath := filepath.Join(pluginDir, "run.sh")
    afero.WriteFile(fs, scriptPath, []byte(content), 0755)
}
```

### Integration Tests

- **Plugin discovery on real filesystem**: Create actual plugin directories with executable scripts in a temp dir, verify discovery, execution, and env var propagation.
- **Plugin subprocess lifecycle**: Verify context cancellation kills plugin processes, non-zero exits propagate error codes, and stdout/stderr are captured.
- Gate with `testutil.SkipIfNotIntegration(t, "plugins")` in a dedicated `plugins_integration_test.go` file.

### E2E BDD Tests (Godog) — **Strong fit**

Plugin discovery and execution are user-facing CLI workflows. Feature file: `features/cli/plugins.feature`.

```gherkin
@cli @integration
Feature: CLI Plugin System
  Plugins are discovered from the plugins directory and
  registered as CLI subcommands.

  Background:
    Given the gtb binary is built

  Scenario: Plugin appears in help output
    Given a plugin "hello" with a manifest and script that prints "Hello from plugin"
    When I run gtb with "--help"
    Then the exit code is 0
    And stdout contains "hello"

  Scenario: Plugin executes successfully
    Given a plugin "hello" with a manifest and script that prints "Hello from plugin"
    When I run gtb with "hello"
    Then the exit code is 0
    And stdout contains "Hello from plugin"

  Scenario: Plugin with non-zero exit propagates error
    Given a plugin "fail" with a script that exits with code 42
    When I run gtb with "fail"
    Then the exit code is not 0

  Scenario: Missing plugins directory is not an error
    When I run gtb with "--help"
    Then the exit code is 0
```

Plugin step definitions will need a Given step that creates a temp plugins directory with executable scripts and configures the binary to look there (via `--config` or env var).

### Coverage
- Target: 90%+ for `pkg/plugins/`.

---

## Linting

- `golangci-lint run --fix` must pass.
- No new `nolint` directives.

---

## Documentation

- Godoc for all exported types in `pkg/plugins/`.
- User-facing documentation in `docs/components/plugins.md`:
  - How to create a plugin
  - Plugin directory structure
  - Manifest format reference
  - Environment variables available to plugins
  - Security considerations
- Update `docs/components/features.md` with `PluginsCmd` feature flag.

---

## Backwards Compatibility

- **No breaking changes**. Plugins are disabled by default via feature flag.
- Tools without a plugins directory are unaffected.
- No existing commands are modified.

---

## Future Considerations

- **Plugin marketplace**: A `plugin install` command that downloads plugins from a registry (GitHub releases, custom registry).
- **Plugin versioning**: Semantic version constraints in the manifest for compatibility with parent tool versions.
- **Structured I/O**: JSON-over-stdin/stdout protocol for richer plugin interactions (like VS Code extensions).
- **Plugin hooks**: Allow plugins to register as pre/post hooks for built-in commands.
- **Go plugin support**: If Go's plugin package improves, compiled Go plugins could offer better performance and type safety.

---

## Implementation Phases

### Phase 1 — Core Types
1. Create `pkg/plugins/` package with types
2. Add `PluginsCmd` feature flag
3. Implement `Discoverer` interface and `fsDiscoverer`

### Phase 2 — Execution
1. Implement `subprocessExecutor`
2. Define environment variable contract
3. Add Cobra command registration

### Phase 3 — Integration
1. Wire `RegisterPlugins` into root command
2. Feature flag gating
3. Warning on first plugin discovery

### Phase 4 — Tests & Documentation
1. Add discovery tests with afero
2. Add execution tests with test scripts
3. Write user-facing documentation

---

## Verification

```bash
go build ./...
go test -race ./pkg/plugins/...
go test ./...
golangci-lint run --fix

# Manual verification
mkdir -p ~/.toolname/plugins/hello
echo '#!/bin/bash\necho "Hello from plugin!"' > ~/.toolname/plugins/hello/run.sh
chmod +x ~/.toolname/plugins/hello/run.sh
echo 'name: hello\ndescription: Says hello\nversion: 1.0.0' > ~/.toolname/plugins/hello/plugin.yaml

# Enable plugins feature and run
go run . hello  # should print "Hello from plugin!"
```
