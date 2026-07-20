---
title: How to Test Code That Uses Configuration
description: Build in-memory test stores, use the generated config mocks, and test observers.
date: 2026-02-16
tags: [how-to, config, testing, mocks, observers]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Test Code That Uses Configuration

The `go/config` Store is built for testability: reads go through the
`config.Reader` interface (which the published mocks implement), and a store
can be fed entirely from an in-memory document — no files on disk. This guide
covers the common recipes. For general test scaffolding (test `Props`,
filesystem mocking, race avoidance), see [How to Test Components](testing.md);
for the design, see the [Config component](../explanation/components/config/index.md).

## Build a test configuration in memory

Feed a store from a named in-memory source and pin a view:

```go
func TestMyFunction(t *testing.T) {
    store, err := config.NewStore(t.Context(),
        config.WithReaders(config.NamedSource{Name: "test", Content: []byte(`
app:
  name: "test-app"
  debug: true
  port: 8080
database:
  host: "localhost"
  port: 5432
`)}),
    )
    require.NoError(t, err)

    result := MyFunctionThatNeedsConfig(store.View())
    assert.Equal(t, "expected", result)
}
```

A `*config.Store` satisfies `props.Props.Config` directly, so the same
construction backs a full test `Props`. When a test needs a **writable** layer
(anything exercising `Apply`, or provenance checks that distinguish
user-authored files from embedded defaults), declare a real file layer instead:

```go
fs := afero.NewMemMapFs()
require.NoError(t, afero.WriteFile(fs, "config.yaml", []byte("log:\n  level: info\n"), 0o600))

store, err := config.NewStore(t.Context(),
    config.WithFiles(configafero.Wrap(fs), "config.yaml"))
```

Inside this repository, `internal/testutil` wraps both recipes as
`StoreFromYAML` / `ViewFromYAML` and `FileStoreFromYAML` / `FileViewFromYAML`.

## Use the generated mocks

The config module publishes mockery-generated mocks in `go/config/mocks`.
**Prefer these over hand-written fakes** — they are generated from the real
interfaces (`MockReader`, `MockObservable`, `MockBinder`), stay in sync, and
verify expectations on cleanup. A `MockReader` is the right double for any
function that takes `config.Reader`:

```go
import (
    "testing"

    configmocks "gitlab.com/phpboyscout/go/config/mocks"
    "github.com/stretchr/testify/assert"
)

func TestWithProvidedMocks(t *testing.T) {
    mockConfig := configmocks.NewMockReader(t)

    mockConfig.EXPECT().GetString("database.host").Return("test-host")
    mockConfig.EXPECT().GetInt("database.port").Return(5432)
    mockConfig.EXPECT().Has("database.ssl").Return(true)
    mockConfig.EXPECT().GetBool("database.ssl").Return(false)

    service := NewDatabaseService(mockConfig)
    assert.NoError(t, service.Connect())
    // Expectations are verified automatically on cleanup.
}
```

Typed-section consumers (`config.UnmarshalSection`, generated
`Validate<Name>Config` functions) also take `config.Reader`, so they stay
mockable: expect `SectionExists` and drive `UnmarshalKey` with `RunAndReturn`
to populate the target struct.

## Test observer behaviour

Observers often carry critical logic (restarting services, changing log
levels) and signal validation errors via their returned `error`. You don't
need file watching — drive a reload deliberately.

**Observer logic in isolation** — `Run` takes `config.Observed`, and a store
satisfies the read surface, so hand it a pinned snapshot however you like and
call it directly.

**Registration + reload, with a mutable source:** give the store a backend
whose content the test can replace, then call `Reload`:

```go
type mutableSource struct {
    mu      sync.Mutex
    content []byte
}

func (m *mutableSource) ID() string                          { return "test.yaml" }
func (m *mutableSource) Capabilities() config.Capabilities   { return config.Capabilities{} }
func (m *mutableSource) Set(yaml string)                     { m.mu.Lock(); m.content = []byte(yaml); m.mu.Unlock() }
func (m *mutableSource) Load(ctx context.Context, _ []config.Layer) ([]config.Layer, error) {
    m.mu.Lock()
    content := m.content
    m.mu.Unlock()

    return config.NewReaderBackend("test.yaml", content).Load(ctx, nil)
}

func TestObserverFiresOnReload(t *testing.T) {
    src := &mutableSource{content: []byte("log:\n  level: info\n")}
    store, err := config.NewStore(t.Context(), config.WithBackend(src))
    require.NoError(t, err)

    var got string
    store.AddObserverFunc(func(cfg config.Observed) error {
        got = cfg.GetString("log.level")
        return nil
    })

    src.Set("log:\n  level: debug\n")
    require.NoError(t, store.Reload(t.Context()))
    assert.Equal(t, "debug", got, "the observer sees the snapshot that triggered it")
}
```

An unchanged reload does not notify — observers fire only when the resolved
configuration actually changed. Inside this repository the mutable-source
recipe is available as `testutil.MutableStoreFromYAML`.

## Inspect and debug configuration in a test

When values aren't resolving as expected, ask the store where a value came
from — provenance is first-class:

```go
view := store.View()

fmt.Println(view.Explain("database.host")) // which layer won, and who was shadowed
snapshot := store.Snapshot()
allValues := snapshot.Values()             // the fully-resolved tree
layers := snapshot.Layers()                // every loaded layer with its Source
```

Resolution precedence when a value surprises you: **changed flags → env →
config files (later files override earlier) → embedded defaults**. For
schema-based validation see
[Validate configuration](https://config.go.phpboyscout.uk/how-to/validate-config/);
for general runtime issues see the
[Troubleshooting Guide](../development/troubleshooting.md).

## Related

- [How to Test Components](testing.md) — test `Props`, filesystem mocking, race avoidance
- [Config component](../explanation/components/config/index.md) — the Store design and GTB integration
- [How to React to Configuration Changes](config-hot-reload.md) — production hot-reload
