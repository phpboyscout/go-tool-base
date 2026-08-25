---
title: "Implementation notes, config hot-reload rework"
description: "What was built for the container-owned hot-reload watcher, deviations from the spec, and open questions for review."
date: 2026-06-12
tags: [implementation-notes, config, hot-reload, concurrency]
---

# Implementation notes: config hot-reload rework

Spec: [`0074-config-hot-reload-rework`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0074-config-hot-reload-rework)

## What was implemented

All six findings closed via a container-owned `fsnotify` watcher in `pkg/config`.

- **D1: Container owns the watcher.** `Container` now carries `fs`, `configFiles`,
  `reloadDebounce`, a `*fsnotify.Watcher`, a `watchDone` channel, and a `sync.Mutex`.
  `watchConfig()` creates one watcher and `Add`s every configured file, then runs
  `watchLoop` in a goroutine. Viper's `WatchConfig`/`OnConfigChange` are no longer used.
- **D2: Candidate-validate-swap.** `reload()` calls `buildCandidate(fs, files)` which
  reads `file[0]` then `MergeInConfig`s `files[1:]` into a fresh viper built by the new
  shared `newResolverViper` helper (same afero fs, env prefix, AutomaticEnv, key replacer,
  type-by-default as the live viper). If a schema is set, the candidate is validated via the
  new `validateViper(v, schema)`; only on success is `c.viper` swapped under the lock.
- **D3: Returned-error observer contract.** `Observable.Run(Containable) error` and
  `AddObserverFunc(func(Containable) error)`. The unbuffered/unread `errs` channel and the
  per-observer goroutine + `WaitGroup` are gone. Observers run sequentially in
  registration order; a returned error is logged and never blocks subsequent observers or
  reloads.
- **D4: Race-safety.** `observers`, `schema`, and the live `viper` pointer are all guarded
  by the container mutex. All `Get*`/`Has`/`IsSet`/`Set`/`Sub`/`WriteConfigAs`/`ToJSON`/
  `Validate` reads route through `liveViper()` (reads the pointer under lock). `notify()`
  copies the observer slice under lock and iterates the copy outside the lock. The watcher
  is started only after construction completes (see below). Verified clean under
  `go test -race` (including a concurrent-AddObserver-during-reload test).
- **D5: Single-file watch.** `watchConfig()` moved out of the `len(files) > 1` branch; it
  is now called for every file-backed container. The previously-vacuous single-file observer
  test now requires `observed > 0` (polled with `require.Eventually`).
- **D6: Configurable debounce + re-watch.** `WithReloadDebounce(d)` (default
  `DefaultReloadDebounce` = 250 ms). `watchLoop` coalesces the event burst behind a single
  timer; `rewatch()` removes and re-`Add`s the affected path after every event so
  atomic-rename saves are not missed.
- **D7: Fail-closed partial merge.** `buildCandidate` returns an error if any file fails to
  parse/merge (or if `file[0]` is missing, honouring `ErrConfigFileNotFound`); `reload`
  then keeps last-known-good and does not swap.

### Watcher start points

`watchConfig()` is invoked **after** construction completes, in `NewFilesContainer`,
`LoadFilesContainer`, and `LoadFilesContainerWithSchema` (the last after schema is set), so
the reload goroutine never observes a half-built container.

### New public surface

- `func WithReloadDebounce(time.Duration) ContainerOption`
- `const DefaultReloadDebounce = 250 * time.Millisecond`
- `func (c *Container) Close() error`: stops the watcher; idempotent; safe on non-watching
  containers.

## Observer signature change (before / after)

```go
// Before
type Observable interface { Run(Containable, chan error) }
cfg.AddObserverFunc(func(c config.Containable, errs chan error) { errs <- err })

// After
type Observable interface { Run(Containable) error }
cfg.AddObserverFunc(func(c config.Containable) error { return err })
```

Migration note: [`docs/migration/v0.16-hot-reload-observer.md`](../../reference/migration/v0.16-hot-reload-observer.md)
(also linked from `docs/migration/index.md`). This is a breaking change; commit carries a
`BREAKING CHANGE:` footer. Pre-1.0, so it ships as a minor bump with no shim.

## Deviations from the spec (and why)

1. **"Notify observers of the error" on a rejected reload.** The spec (D2/D3/D7) says to
   notify observers of the error on a failed reload. With the returned-error contract there
   is no longer a channel to *push* an error into an observer, observers *return* errors,
   they don't receive them. Notifying observers on a rejected reload would hand them the
   unchanged last-known-good config and imply a change occurred. The implementation therefore
   **logs** the reload error (container's responsibility) and does **not** call observers when
   nothing changed. The verifiable guarantees, `Get*` serves last-known-good and the bad
   config is never visible: are fully met and tested. Flagged as an open question below.

2. **Observers run sequentially, not in parallel.** The old code spawned a goroutine per
   observer; the new code runs them in registration order on the watch goroutine. This matches
   the documented "registration order" semantics, removes a goroutine/WaitGroup per reload, and
   keeps the contract simple. Expensive observers should still offload work (documented in the
   how-to).

## Tests added / changed

`pkg/config/hotreload_test.go` (new): merge-preservation, validation-rollback, partial-merge
fail-closed, primary-file-removed fail-closed, observer-error-no-deadlock, env-prefix
preserved across reload, Close idempotency, and a `-race` concurrent-observer test.
`pkg/config/container_test.go`: `TestObserver` and the single-file watch test updated to the
new signature and a real `observed > 0` assertion. Mocks regenerated (`go tool mockery`).

Coverage on the changed surface: most new functions 80–100%; remaining gaps are defensive
fault-injection paths (fsnotify error channel, `watcher.Add`/`Remove` failures,
`ReadInConfig` mid-reload errors) that need a fake watcher/fs to hit deterministically.

## Open questions for review

1. **Rejected-reload notification (deviation 1)., RESOLVED.** Per review, an
   `OnReloadError(func(error))` hook was added to the file-backed container (and to the
   `Containable` interface; mocks regenerated). It is the faithful realization of the spec's
   "notify observers of the error" intent under the returned-error contract: observers
   *return* errors (and so cannot *receive* a pushed reload-time error), so a dedicated
   reload-error hook lets embedders react to a rejected reload programmatically. The hook
   fires on every rejected reload: a fail-closed candidate-build failure (partial-merge /
   parse error / missing primary file) or a schema-validation failure, where last-known-good
   is retained; it is *not* called on a successful reload (observers are). Callbacks are stored
   in a mutex-guarded slice, copied under the lock and invoked outside it (the same discipline
   as `notify()`), so concurrent registration during active reloads is race-safe. The container
   still logs the rejection at `ERROR`; the hook is additive. Documented in
   `docs/components/config.md` (§ Reacting to rejected reloads), `docs/how-to/config-hot-reload.md`,
   and the migration note. Tests: validation-rejection fires the hook, fail-closed partial-merge
   fires the hook, a successful reload does *not* fire it, and a `-race` concurrent-registration
   test.

2. **Reader/embedded containers.** Confirmed out of scope and unaffected, `NewReaderContainer`
   sets no `configFiles`, so `watchConfig()` is a no-op for it. `Close()` is also a no-op
   there.

3. **Test timing tolerance.** Hot-reload tests use a small injected debounce
   (`WithReloadDebounce(20ms)`) and poll with `require.Eventually`/`require.Never` rather than
   hard-sleeping. They are green across repeated `-race` runs locally, but are inherently
   filesystem/timer-sensitive; CI on a heavily loaded runner may need the generous timeouts
   (currently 3 s eventually / 1 s never) kept or raised.

4. **Migration-note location/versioning.** The note is filed as
   `docs/migration/v0.16-hot-reload-observer.md` and added to the migration index with a
   `v0.16 | —` row (no fixed target version, since releaser-pleaser computes the bump).
   Confirm the naming/versioning convention is acceptable.

5. **Debounce default validation.** `WithReloadDebounce(d)` and the option pipeline treat
   `d <= 0` as "use default 250 ms". There is no upper bound. Confirm no max clamp is desired.
