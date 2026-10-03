---
title: Build a Config Source in Your Own Code
description: How to supply the backend for a declared config source slot from your tool's main package, for the etcd, sftp, billy, iofs and afero kinds and for any slot you want to build yourself.
date: 2026-10-03
tags: [how-to, config, sources, override]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Build a Config Source in Your Own Code

Most config source kinds build themselves from settings each user writes with
`<tool> init config <name>`. Five cannot: `etcd` and `sftp` have no convention
to read a connection from, and `billy`, `iofs` and `afero` read from a
filesystem that only your code holds. For those, and for any slot whose shipped
kind does not fit, you register an **override**: a function that builds the
slot's backend, called in the slot's place in the stack.

The generator declares the slot and never writes the override. The override
lives in a file of your own, so regenerating leaves it alone.

## 1. Declare the slot

Declare it in the wizard's Configuration page, or with the generate flags:

```bash
gtb generate project --config-source legacy=etcd --config-source team=iofs ...
```

On an existing project, add the slot to `properties.config.sources` in
`.gtb/manifest.yaml` and run `gtb regenerate project`. The slot's name is what
the override registers against, and its place in `properties.config.layers` is
its precedence.

## 2. Add the adapter to your module

Each override-only kind has an adapter in the go/config family:

| Kind | Module |
|---|---|
| `etcd` | `gitlab.com/phpboyscout/go/config-etcd` |
| `sftp` | `gitlab.com/phpboyscout/go/config-sftp` |
| `billy` | `gitlab.com/phpboyscout/go/config-billy` |
| `iofs` | `gitlab.com/phpboyscout/go/config-iofs` |
| `afero` | `gitlab.com/phpboyscout/go/config-afero` |

```bash
go get gitlab.com/phpboyscout/go/config-etcd
```

## 3. Register the override

Create a file in your `cmd/<name>/` package, for example `cmd/mytool/sources.go`,
and call `setup.OverrideConfigSource` from `init`. The factory is given the
slot's settings, the `config.sources.<name>` subtree of the user's config (nil
when nobody has set any), and a bootstrap with the tool's filesystem and its
linked codecs.

An etcd slot that reads its endpoints from the user's config:

```go
package main

import (
	"context"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"

	"gitlab.com/phpboyscout/go/config"
	configetcd "gitlab.com/phpboyscout/go/config-etcd"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func init() {
	setup.OverrideConfigSource("legacy", func(_ context.Context, settings config.Reader, _ setup.ConfigBootstrap) (config.Backend, error) {
		cfg := clientv3.Config{DialTimeout: 5 * time.Second}
		prefix := "/mytool/"

		if settings != nil {
			cfg.Endpoints = settings.GetStringSlice("endpoints")

			if p := settings.GetString("prefix"); p != "" {
				prefix = p
			}
		}

		return configetcd.FromConfig(cfg, prefix)
	})
}
```

The user then sets `config.sources.legacy.endpoints` in their own config file.
With none set, `FromConfig` refuses, and a required slot stops the tool naming
it.

A slot that reads a file compiled into the binary, in whichever format its
extension names:

```go
//go:embed defaults/team.yaml
var team embed.FS

func init() {
	setup.OverrideConfigSource("team", func(_ context.Context, _ config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
		const path = "defaults/team.yaml"

		codec, err := b.CodecFor(path)
		if err != nil {
			return nil, err
		}

		return config.NewCodecBackend(configiofs.Wrap(team), path, codec), nil
	})
}
```

## What the framework does with it

- The override replaces the slot's factory and nothing else. Required,
  writable and precedence still come from the manifest.
- The factory reads its settings from the embedded defaults, the user's own
  files, the environment and flags, never the project file, so a repository
  cannot redirect it.
- An override for a slot the tool does not declare stops the tool at
  startup, so a stale one cannot add a layer.
- A slot of an override-only kind with no override stops the tool, naming
  `setup.OverrideConfigSource`.

See [Configure a tool's config stack](configure-the-config-stack.md) for
declaring, configuring and ordering slots, and
[Config sources](../explanation/components/config/index.md#config-sources) for
how slots, kinds and the two-pass store fit together.
