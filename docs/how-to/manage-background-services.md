---
title: How to Manage Background Services
description: Run long-lived services under a go/controls controller inside a command, and make the tool's exit code say how they ended.
date: 2026-02-17
tags: [how-to, controls, services, background, lifecycle]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# How to Manage Background Services

Background services (API listeners, file watchers, long-running workers) need
ordered start-up, health monitoring and graceful shutdown. A tool built on GTB
runs them under a controller from the standalone
[`gitlab.com/phpboyscout/go/controls`](https://controls.go.phpboyscout.uk)
module, inside the command that serves them. GTB consumes the module directly,
and the glue is `Props` and `logger.ToSlog`.

This guide needs go/controls v0.8.0 or later. For the full supervisor API, see
[controls.go.phpboyscout.uk](https://controls.go.phpboyscout.uk).

## 1. Create the controller inside the command

Build the controller in the command's `RunE`, on `cmd.Context()`:

```go
func newServeCommand(props *props.Props) *setup.Command {
    cmd := &cobra.Command{
        Use:   "serve",
        Short: "Run the service until stopped",
        RunE: func(c *cobra.Command, _ []string) error {
            controller := controls.NewController(c.Context(),
                controls.WithLogger(logger.ToSlog(props.Logger)),
                controls.WithStopOnGiveUp(),
            )

            registerHTTPServer(controller, props)
            registerWorker(controller, props)

            controller.Start()
            <-controller.Done()

            return outcomeError(controller)
        },
    }

    return setup.Wrap("", cmd)
}
```

Three things here are deliberate:

- **No `controls.WithSignals()`.** The root command owns `SIGINT` and `SIGTERM`.
  It cancels `cmd.Context()`, and the controller shuts down through its normal
  sequence. A second handler would race the root's on a single Ctrl-C. See
  [the Controls component](../explanation/components/controls/index.md).
- **`WithStopOnGiveUp()`.** By default a service that fails for good is logged,
  and the process stays up but not ready. With this option, the controller
  stops, so the command can end and the tool can exit non-zero.
- **The command returns the outcome.** Step 5 turns the controller's stop cause
  into the command's result, and the root sets the exit code from that.

## 2. Register a server

`WithStart` starts the service, `WithStop` shuts it down within the shutdown
deadline, and `WithStatus` is a health probe that returns `nil` while healthy.
A server that listens in the background returns `nil` from its start once it's
listening:

```go
func registerHTTPServer(controller *controls.Controller, props *props.Props) {
    server := &http.Server{Addr: props.Config.View().GetString("server.addr"), Handler: mux()}
    serveErr := make(chan error, 1)

    controller.Register("http-server",
        controls.WithStart(func(context.Context) error {
            ln, err := net.Listen("tcp", server.Addr)
            if err != nil {
                return errors.Wrap(err, "listening")
            }

            go func() { serveErr <- server.Serve(ln) }()

            return nil
        }),
        controls.WithStop(func(ctx context.Context) {
            if err := server.Shutdown(ctx); err != nil {
                props.Logger.Error("HTTP server shutdown", "error", err)
            }
        }),
        controls.WithStatus(func() error {
            select {
            case err := <-serveErr:
                return errors.Wrap(err, "HTTP server stopped serving")
            default:
                return nil
            }
        }),
    )
}
```

With `WithStopOnGiveUp`, the controller polls that status after a clean start.
This is the **death watch**, and it gives up when the server dies. The default
polls every 10 seconds and gives up after three failures in a row.
`controls.WithDeathWatch(interval, threshold)` changes that.

## 3. Register a worker

A worker's start blocks for as long as the worker runs. It returns `nil` when
its context ends, and an error when it fails:

```go
func registerWorker(controller *controls.Controller, props *props.Props) {
    controller.Register("worker",
        controls.WithStart(func(ctx context.Context) error {
            ticker := time.NewTicker(30 * time.Second)
            defer ticker.Stop()

            for {
                select {
                case <-ctx.Done():
                    return nil
                case <-ticker.C:
                    if err := doWork(ctx, props); err != nil {
                        return errors.Wrap(err, "worker")
                    }
                }
            }
        }),
        controls.WithRestartPolicy(controls.RestartPolicy{MaxRestarts: 3}),
    )
}
```

A returned error is a failure. The restart policy retries it, and once the
retries are exhausted the service gives up. Never send on `controller.Errors()`:
the controller is that channel's only sender.

## 4. Watch failures and readiness

`WithOnEvent` delivers each retry and each terminal failure, in order, on a
goroutine of its own:

```go
controls.WithOnEvent(func(ev controls.ServiceEvent) {
    props.Logger.Warn("service failure",
        "service", ev.Service, "kind", ev.Kind, "restarts", ev.Restarts, "error", ev.Err)
}),
```

Add it to `NewController`'s options. `controller.Readiness()` returns the
aggregated health report. It's ready only while the controller is running and
every check passes, so a health endpoint or a ticker can read it.

## 5. Turn the outcome into the command's result

When `Done()` closes, `Outcome()` says why the controller stopped. Decide on
`Cause` before reading `Err`, because under `CauseGaveUp` the error can be
`context.Canceled`:

```go
func outcomeError(controller *controls.Controller) error {
    outcome, _ := controller.Outcome()

    if outcome.Cause == controls.CauseGaveUp {
        return errors.Wrapf(outcome.Err, "service %s gave up", outcome.Service)
    }

    return nil
}
```

| Cause | Why the controller stopped | The command returns | The tool exits |
|---|---|---|---|
| `CauseGaveUp` | a service failed for good | an error | 1 |
| `CauseCompleted` | a service declared with `WithRunToCompletion` finished | `nil` | 0 |
| `CauseParent` | `cmd.Context()` ended, usually a signal the root handled | `nil` | 128+signum, set by the root |
| `CauseStop` | something called `controller.Stop()` | `nil` | 0 |

A service that's a unit of work rather than a long-lived one registers with
`controls.WithRunToCompletion()`. Its `nil` return then stops the controller
with `CauseCompleted`.

## A panic is a failure, not a crash

From go/controls v0.8.0, a panic in a start, a status poll or a health check is
that service's failure, wrapping `controls.ErrPanicked`. It no longer crashes
the process. Without `WithStopOnGiveUp`, a service that panics on start becomes
unable to start, and the process stays up but not ready. If you relied on the
crash to restart the tool, the setup in step 1 replaces it: the give-up stops
the controller and the tool exits 1. `controls.WithOnPanic` hands each panic to
a callback, for an error tracker.

## Run it under a process manager

Nothing extra is needed for the command to start under systemd or in a pod. With
no terminal on stdin the root treats the run as unattended: it makes no update
check, asks nothing, and starts without a config file on the defaults and the
environment. If the unit or manifest gives the service a terminal (`tty: true`,
`docker run -t`), pass `--ci` so it's still treated as unattended. See
[Unattended runs](../explanation/components/setup/root-command.md#unattended-runs).

The exit code from step 5 is what lets the process manager act: `Restart=on-failure`
in systemd, or a pod's restart policy, restarts a tool that exits 1 when a
service gives up.

## Related

- [Controls component](../explanation/components/controls/index.md): how GTB consumes the module, and why the root owns signals
- [Stop when a service gives up](https://controls.go.phpboyscout.uk/how-to/stop-on-give-up/): the module's own guide to give-up and completion
- [controls.go.phpboyscout.uk](https://controls.go.phpboyscout.uk): the full supervisor API, service types, health model and testing guide
- [Register health checks](register-health-checks.md)
