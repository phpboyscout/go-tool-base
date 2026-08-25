---
title: Testing
description: Testing telemetry collection and backends.
date: 2026-03-31
tags: [components, telemetry, analytics, privacy]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Testing

## Testing

### Unit Tests

Use the noop collector, `Props.Collector` is always non-nil:

```go
p := &props.Props{
    // Collector is nil — telemetry calls are safe but do nothing
}
```

Or create a disabled collector for explicit testing:

```go
c := telemetry.NewCollector(telemetry.Config{}, telemetry.NewNoopBackend(),
    "test", "1.0.0", nil, logger.ToSlog(logger.NewNoop()), "", props.DeliveryAtLeastOnce, false)
```

### Verifying Events

Use a spy backend to capture events in tests:

```go
type spyBackend struct {
    events []telemetry.Event
    mu     sync.Mutex
}

func (s *spyBackend) Send(_ context.Context, events []telemetry.Event) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.events = append(s.events, events...)
    return nil
}

func (s *spyBackend) Close() error { return nil }
```

---
