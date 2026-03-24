---
title: Generating Commands
description: Comprehensive guide on generating new commands, including nesting, flags, and assets.
date: 2026-02-16
tags: [cli, generator, commands]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Generating Commands

### Autonomous Repair Agent

By default, the `generate command` utility uses an **Autonomous Repair Agent** to verify and improve the generated code.

You can trigger AI generation in two ways:
1. **Script Conversion**: Provide an existing script (bash, python, js) using the `--script` flag.
2. **Prompt-Based**: Provide a natural language description using the `--prompt` flag (either as a raw string or a path to a file).

!!! warning "Important"
    `--script` and `--prompt` are mutually exclusive.

Ready to add some muscle to your tool? The `generate command` utility is the most powerful weapon in your arsenal. ⚡

Whether you're adding a simple utility or building a complex command hierarchy, this tool handles the heavy lifting of structure, registration, and documentation.

## Core Features

### 1. Simple Command Generation

To add a new command at the root level:

```bash
go run main.go generate command --name "my-command" --short "Short description"
```

This creates:

- `pkg/cmd/my-command/cmd.go`: The **read-only** command registration and boilerplate.
- `pkg/cmd/my-command/main.go`: The **editable** implementation file where your logic lives.
- `docs/commands/my-command/index.md`: AI-generated documentation for the command.
- An entry in `.gtb/manifest.yaml`.

!!! note "Separation of Concerns"
    We separate the boilerplate (`cmd.go`) from your logic (`main.go`) so that we can regenerate the boilerplate as you add flags or change configurations without overwriting your hard work!

### 2. Hierarchical Nesting 🌳

Organization is key to a great CLI. You can nest commands as deeply as you like using the `--parent` flag.

```bash
# Create 'dog' under 'root'
go run main.go generate command -n dog --parent root

# Create 'cat' under 'dog'
go run main.go generate command -n cat --parent dog

# Create 'mouse' under 'dog/cat'
go run main.go generate command -n mouse --parent dog/cat
```

### 3. Interactive Mode 🎛️

Running `generate command` without any flags launches a **multi-stage interactive wizard**, similar to `generate project`. The wizard walks you through the core fields first (name, description, parent, options), then conditionally presents additional stages based on your choices:

- **Flag Definition stage**: Enabled by selecting "Add Flags" in the main form. Each flag has its own dedicated form with one field per property. After submitting a flag you can tick "Add Another Flag" to keep adding more. A running summary of already-defined flags is shown at the top of each subsequent form for reference.
- **AI Prompt stage**: Enabled by selecting "Set AI Prompt" in the main form.

### 3a. Adding Flags via CLI 🚩

You can define flags during generation using the `--flag` (`-f`) argument. The full format is `name:type:description:persistent:shorthand:required:default:defaultIsCode`.

Only `name`, `type`, and `description` are required; trailing fields can be omitted.

```bash
# Add a simple string flag
go run main.go generate command -n greet -f "name:string:Name to greet"

# Add multiple flags
go run main.go generate command -n server \
  -f "port:int:Port to listen on" \
  -f "verbose:bool:Enable verbose logging"

# Add a flag with a shorthand and default value
go run main.go generate command -n fetch \
  -f "timeout:int:Request timeout in seconds:false:t:false:30"
```

To make a flag **persistent** (available to this command and all subcommands), set the fourth field to `true`:

```bash
# Add a persistent config flag
go run main.go generate command -n root -f "config:string:Config file:true"
```

Need to add a flag to an existing command? Check out the [Add Flag](add-flag.md) utility!

### 3b. Smart Asset Handling 📦

Need configuration files or static assets? Use the `--assets` flag.

- The CLI generates an `assets/` directory with a default `config.yaml`.
- It uses Go's `embed` package to bundle these files into your binary.
- Registration logic automatically adds these assets to the `Props` container via `p.Assets.Add(&assets)`.

### 4b. Lifecycle Hooks ⚙️

You can generate lifecycle hook stubs for your command:

- `--persistent-pre-run`: Generate a `PersistentPreRun` hook (runs before this command and all subcommands).
- `--pre-run`: Generate a `PreRun` hook (runs before this command only).
- `--with-initializer`: Generate a config Initializer for this command (creates `init.go` and adds an `Init<Name>` stub in `main.go`).

```bash
# Generate a command with a PersistentPreRun hook and an initializer
go run main.go generate command -n serve --persistent-pre-run --with-initializer
```

!!! note "Options are authoritative"
    The options you pass to `generate command` are always written directly to the manifest. If you re-run the command **without** an option that was previously enabled, that option is removed from the manifest and its associated files are cleaned up. For example, omitting `--with-initializer` on a subsequent run will remove `init.go` from the command directory.

!!! tip "Updating existing commands"
    If you add a lifecycle hook option to a command whose `main.go` already exists (and you're not using `--force`), the generator will **append the missing stub** to your existing implementation file rather than overwriting it. It will also inject any required imports. Your existing logic is never modified.

### 5. Command Protection 🛡️

Once you've generated a command, you might want to prevent it from being overwritten by future `generate` calls—especially if you've made manual modifications to the boilerplate (though we recommend strictly keeping logic in `main.go`!).

**Protection Tri-State Logic:**

- **Protected**: The command cannot be overwritten. Generation will fail with an error.
- **Unprotected**: The command can be overwritten/updated safely.
- **Default (Unset)**: Respects existing status; if previously protected, it stays protected.

**During Generation:**
You can mark a command as protected right from the start:
```bash
go run main.go generate command -n critical-cmd --protected
```

**Managing Protection:**
You can toggle protection for existing commands using the `protect` and `unprotect` subcommands:

```bash
# Lock a command
go run main.go generate command protect path/to/command

# Unlock (allow overwrite)
go run main.go generate command unprotect path/to/command
```

### 6. Dry-Run Mode 🔍

Want to preview what `generate command` will do before it writes anything? Use the `--dry-run` flag:

```bash
go run main.go generate command -n my-command --dry-run
```

This shows you:

- **Files to create**: New files that would be generated, with their paths listed.
- **Files to modify**: Existing files that would change, with a unified diff showing exactly what would be different.

Dry-run mode runs the full generation pipeline—including `go mod tidy` and `golangci-lint run --fix`—in a temporary directory to ensure the preview is accurate. The only step skipped is AI-powered documentation generation, since that requires an API call.

!!! tip "Use with regenerate too"
    The `--dry-run` flag is also available on `regenerate project` and `generate skeleton`. See the [Regeneration](regenerate.md) and [Skeleton](skeleton.md) docs for details.

### 7. Command Safety & Manual Edits 🛡️

The generator is designed to manage the lifecycle of your `cmd.go` files (boilerplate) while you own the `main.go` files (logic).

**What if I edit `cmd.go` manually?**

If you modify a generated `cmd.go` file (e.g., to add a quick flag or change a description manually), the generator will detect this deviation on the next run.

1.  **Hash Verification**: The generator stores a hash of the generated content in `manifest.yaml`.
2.  **Change Detection**: On regeneration, it compares the current file's hash against the stored hash.
3.  **Conflict Resolution**:
    - If a manual change is detected, the generator will **pause and prompt** you: "The file cmd.go has been modified... Do you want to overwrite it?"
    - You can choose `No` to keep your changes (stalling the generator for that file) or `Yes` to overwrite them with the standard boilerplate.

**Bypassing the Check**:
If you are sure you want to overwrite manual changes (e.g., in a CI/CD pipeline or during a refactor), use the `--force` flag:

```bash
go run main.go generate command -n my-cmd --force
```

### AI-Powered Script Conversion or Prompting

You can use the AI to either convert an existing script (bash, python, etc.) to Go, or implement functionality from a text description.

<video controls autoplay loop muted playsinline width="100%">
  <source src="../../tapes/prompt-demo.mp4" type="video/mp4">
</video>

-   `--script string`: Path to a script to convert.
-   `--prompt string`: Natural language description or path to a file containing the description.
-   `--agentless`: Opt-out of the autonomous repair agent and use the legacy retry loop.

```bash
go run main.go generate command -n convert --script "./my-script.py" --provider gemini
```

This feature uses an **Autonomous Agent** to convert your logic into idiomatic Go and runs a self-healing verification loop to ensure the code is production-ready. You can use the `--agentless` flag to opt-out of the autonomous agent and use the legacy retry loop instead.

Check out the dedicated [AI Script Conversion](ai-conversion.md) page for more details on how this works!

## Handling Generation Errors

While we strive for perfection, things can go wrong. Here's how to handle common scenarios:

### 1. "Command is protected"
If you see this error, it means you're trying to overwrite a command that was previously generated with `--protected` (or explicitly protected later).

- **Option A**: Use the `unprotect` command: `go tool-base generate command unprotect path/to/cmd`
- **Option B**: Use the force flag (one-time override): `--force --protected=false`

### 2. Linting Failures
The generator runs `golangci-lint run --fix` automatically. If this step fails, **don't panic**.

- The code is still generated and saved.
- You can manually run `golangci-lint run` in your project root to see and fix the specific issues.

For more detailed help, see the [Troubleshooting Guide](../troubleshooting.md).

## Resolving Name Ambiguities

If you have two commands with the same name in different branches, use path-based targeting for the `--parent` flag:

- `--parent /cat`: Targets the 'cat' command directly under root.
- `--parent dog/cat`: Targets the 'cat' command that is a child of 'dog'.

### 8. Positional Arguments 🎯

Cobra provides excellent support for validating positional arguments, and we've exposed that directly in the generator.

You can specify validation rules using the `--args` flag:

```bash
# Require exactly one argument
go run main.go generate command -n echo --args "ExactArgs(1)"

# Require at least one argument
go run main.go generate command -n delete --args "MinimumNArgs(1)"

# Allow any arguments
go run main.go generate command -n run --args "ArbitraryArgs"
```

Common validators include:

- `NoArgs`: The command accepts no arguments.
- `ArbitraryArgs`: The command accepts any arguments.
- `MinimumNArgs(int)`: The command accepts at least N arguments.
- `MaximumNArgs(int)`: The command accepts at most N arguments.
- `ExactArgs(int)`: The command accepts exactly N arguments.

## Documentation Magic

Every time you generate a command, the CLI automatically attempts to generate comprehensive documentation using AI.

- **AI Generation**: The generator reads your `main.go` and uses AI to create an `index.md` file in `docs/commands/your-command/`.
- **Fallback**: If AI generation fails (e.g., due to API issues), it falls back to a clean boilerplate.
- **Updates**: Use the `--update-docs` flag to force a refresh of the documentation using AI.

This ensures your users always have up-to-date, high-quality information about your tool's capabilities.

## Annotated Code Examples

To help you understand the separation of concerns, here's what the generated code looks like.

### 1. The Registration File (`cmd.go`)

**DO NOT EDIT THIS FILE.** It handles all the wiring for Cobra.

```go
package greet

// ... imports ...

type greetOptions struct {
    Name string // Flag variable defined here
}

func NewCmdGreet(props *props.Props) *cobra.Command {
    opts := &greetOptions{}

    // Register assets if they exist
    props.Assets.Add(&assets)

    cmd := &cobra.Command{
        Use:   "greet",
        Short: "Say hello",
        Run: func(cmd *cobra.Command, args []string) {
            // Passes control to your logic in main.go
            props.ErrorHandler.Fatal(RunGreet(cmd.Context(), props, opts, args), "failed to run greet")
        },
    }

    // Flags are automatically wired up to the options struct
    cmd.Flags().StringVar(&opts.Name, "name", "", "Name to greet")
    return cmd
}
```

### 2. The Implementation File (`main.go`)

**THIS IS YOUR FILE.** This is where your code lives.

```go
package greet

import (
    "context"
    "github.com/phpboyscout/go-tool-base/pkg/props"
)

func RunGreet(ctx context.Context, props *props.Props, opts *greetOptions, args []string) error {
    // Access your flags via the opts struct
    props.Logger.Info("Hello!", "name", opts.Name)
    return nil
}
```

This cleaner separation means you can focus purely on the business logic of your command without worrying about CLI framework details. Happy coding! 🚀
