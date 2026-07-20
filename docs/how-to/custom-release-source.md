---
title: How to add a custom release source
description: Register your own forge provider and wire it into a GTB tool's self-update.
date: 2026-07-19
tags: [how-to, vcs, forge, releases, providers]
---

# How to add a custom release source

Release providers live in the standalone
[`forge`](https://forge.go.phpboyscout.uk) module, and a provider ships as **your
own module** — nothing needs contributing to GTB or to forge.

The full authoring guide, including the contract, the credential chain,
credential pinning and the conformance harness, is
**[author a provider](https://forge.go.phpboyscout.uk/how-to/author-a-provider/)**.
This page covers only the GTB side: getting your provider used by a tool's
self-update.

## 1. Register it

Your provider registers itself at `init()`, keyed by any source-type string:

```go
func init() {
    err := forge.Register("s3", func(src forge.ReleaseSourceConfig, cfg forge.Config) (forge.Provider, error) {
        return New(SettingsFromConfig(src, cfg))
    })
    if err != nil {
        panic("myforge: " + err.Error())
    }
}
```

!!! warning "A duplicate source type is an error, not an overwrite"
    `forge.Register` returns `ErrAlreadyRegistered` rather than replacing an
    existing factory — silent overwriting let one blank import displace another
    with no diagnostic, and initialisation order decided the winner.

    To deliberately replace a built-in, call `forge.Unregister` first. To register
    conditionally, check `forge.Registered`.

## 2. Import it for the side effect

Blank-import your module in the tool's `main`, before any update runs:

```go
import _ "example.com/myforge"
```

GTB's own `pkg/setup/providers.go` does this for the first-party set. A tool that
supports only your forge can import just yours and shed the built-in clients
entirely.

## 3. Point the tool at it

```go
props.Tool.ReleaseSource = props.ReleaseSource{
    Type:  "s3",
    Owner: "my-org",
    Repo:  "my-tool",
    Params: map[string]string{"bucket": "releases"},
}
```

`Params` is free-form, so your factory reads whatever keys it needs. The
`vcs.provider` config key overrides `Type` at runtime.

## Injecting directly, without the registry

The registry is process-wide mutable state, so mutating it from tests cannot run
under `t.Parallel()`. For tests — and for a provider constructed at runtime
rather than registered — inject it instead:

```go
setup.NewUpdater(ctx, props, "", false, setup.WithReleaseProvider(myProvider))
```

or set `props.Tool.ReleaseProvider`, which takes precedence over registry lookup.
The option wins over the field.

An injected provider is self-contained, so `NewUpdater` skips **both** the
registry lookup *and* the private-repository token gate that precedes it. That is
why a test double works against a tool configured with `Private: true` and no
credentials — worth knowing before you conclude your credential wiring is
correct because the tests pass.

GTB's own tests drive self-update this way, using the in-memory double from
[`forge/test`](https://forge.go.phpboyscout.uk/how-to/testing/).

## A worked self-update test

```go
func TestSelfUpdate_NoOpWhenLatest(t *testing.T) {
    src := forgetest.New(
        forgetest.WithRelease("v1.2.3", forgetest.TarGzAsset("mytool", "mytool", "#!/bin/sh\n")),
        forgetest.WithLatestTag("v1.2.3"),
    )

    p := &props.Props{
        Tool: props.Tool{
            Name:            "mytool",
            ReleaseProvider: src, // injected: no registry, no token gate
        },
        Logger:  logger.NewNoop(),
        FS:      afero.NewMemMapFs(),
        Version: version.NewInfo("v1.2.3", "abc123", "2026-01-01"),
    }

    updater, err := setup.NewUpdater(t.Context(), p, "", false)
    require.NoError(t, err)

    // Already on the latest version: no download, no replacement.
    require.NoError(t, updater.Update(t.Context()))
}
```

For the fuller picture: `pkg/setup/update_e2e_test.go` drives the verified
pipeline — checksum and signature, happy path and abort — over an in-memory
filesystem, and `features/cli/update.feature` covers the user-visible outcomes
end to end.

## Related

- **[Author a provider](https://forge.go.phpboyscout.uk/how-to/author-a-provider/)** —
  the contract, credential resolution, and the conformance harness
- **[Providers reference](https://forge.go.phpboyscout.uk/reference/providers/)** —
  the first-party set and their config keys
- [Configure self-updating](configure-self-updating.md)
