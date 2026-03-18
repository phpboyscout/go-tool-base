---
title: Adding Custom Commands
description: Implementing and registering custom Cobra commands manually.
date: 2026-02-16
tags: [how-to, commands, custom, implementation]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Adding Custom Commands

While the CLI generator handles most of the boilerplate, it's important to understand how to implement and register commands manually.

## 1. Implement the Command

Create a new package for your command (e.g., `pkg/cmd/greet`). Use a constructor function that accepts `*props.Props`:

```go
func NewCmdGreet(props *props.Props) *cobra.Command {
    return &cobra.Command{
        Use:   "greet [name]",
        Short: "Greets the user",
        Args:  cobra.ExactArgs(1),
        Run: func(cmd *cobra.Command, args []string) {
            name := args[0]
            props.Logger.Info("Hello, " + name)
        },
    }
}
```

## 2. Register the Command

In your `main.go`, pass the created command to `NewCmdRoot`:

```go
func main() {
    // ... setup props

    greetCmd := greet.NewCmdGreet(props)

    // Commands passed here are registered to the root
    rootCmd := root.NewCmdRoot(props, greetCmd)

    rootCmd.Execute()
}
```

## 3. Best Practices

- **Use the Logger**: Always use `props.Logger` instead of `fmt.Println`. This ensures your output respects global flags like `--debug` or `--log-format json`.
- **Handle Errors**: Use `props.ErrorHandler.Fatal(err)` to exit the program with consistent formatting and status codes.
- **Leverage Config**: Use `props.Config` for any user-adjustable settings.
