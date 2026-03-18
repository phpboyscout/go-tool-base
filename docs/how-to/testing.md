---
title: Testing & Mocking
description: Strategies for unit testing commands using mocks and virtual filesystems.
date: 2026-02-16
tags: [how-to, testing, mocking, unit-tests]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing & Mocking

One of the primary goals of GTB is to make CLI tools easily testable. By using the `Props` container, you can inject mock behaviors for filesystems, logging, and configuration.

## Mocking the Filesystem

GTB uses `afero` for filesystem operations. In your tests, you can use `afero.NewMemMapFs()` to simulate a filesystem without touching the disk:

```go
func TestMyCommand(t *testing.T) {
    fs := afero.NewMemMapFs()
    _ = afero.WriteFile(fs, "/config.yaml", []byte("key: value"), 0644)

    props := &props.Props{
        FS: fs,
        // ... other props
    }

    // Now run your command logic using these props
}
```

## Mocking Configuration

The `pkg/config` package provides an in-memory container builder for testing:

```go
cfg := config.NewReaderContainer(logger, "yaml", bytes.NewReader([]byte("key: test-value")))
props.Config = cfg
```

## Best Practices for Tests

- **Avoid Global State**: Do not rely on environment variables or global `os` calls. Use the abstractions provided in `Props`.
- **Table Driven Tests**: Use Go's table-driven test pattern to verify your command logic against multiple input/config scenarios.
- **Capture Output**: You can provide a custom `io.Writer` to the `Logger` in your tests to verify exactly what is being logged.
