---
title: Configure your CLI
description: Give a generated command its own settings, ship defaults with it, override them from the environment and from layered files, ask the user for what you cannot default, and validate what comes back.
date: 2026-10-30
tags: [tutorial, config, validation, initialiser, cli]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Configure your CLI

By the end of this, the `hello` command from
[Build your first CLI](build-your-first-cli.md) reads its greeting from
configuration, ships a default for it, takes overrides from the environment and
from files, and refuses a value it doesn't understand. You'll also add a second
command that asks the user for its setting the first time `init` runs.

Allow about twenty-five minutes.

## Before you start

- **The `mytool` project from [Build your first CLI](build-your-first-cli.md)**,
  with its `hello` command and a config file written by `init`.
- **The same gtb release.** Everything below is verified against **v0.50.0**.

## Read a setting

Open `pkg/cmd/hello/main.go`, your file, and read the greeting from config
instead of hard-coding it:

```go
package hello

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func RunHello(_ context.Context, props *props.Props, _ *HelloOptions, _ []string) error {
	greeting := props.Config.View().GetString("hello.greeting")
	props.Logger.Info(greeting)

	return nil
}
```

`props.Config` is the tool's live configuration. `View()` pins one snapshot of
it, so every read in a call sees the same values even if a file changes while
the command runs. Build and run it:

```bash
just build
./bin/mytool hello
```

```
INFO
```

An empty line. Nothing has set `hello.greeting` yet, and a missing string reads
as empty. The next step gives it a value.

## Ship defaults with the command

A setting that belongs to `hello` should live with `hello`, not in a shared file
you have to remember to edit. Ask the generator for config assets on the command
you already have:

```bash
gtb generate command --name hello --short "Say hello" --assets
```

It's safe to re-run. The generator refreshes the generated `cmd.go`, adds the
asset files, and leaves your `main.go` alone:

```
WARN  .../pkg/cmd/hello/main.go already exists and will not be overwritten. Use --force to overwrite.
```

Don't reach for `--force` here. It rewrites `main.go` too, and that's the file
holding your work.

You now have two files, and they do different jobs:

- **`pkg/cmd/hello/assets/config.yaml`** holds defaults that always apply. They
  sit at the bottom of the stack, under the user's file, the environment and the
  flags.
- **`pkg/cmd/hello/assets/init/config.yaml`** is a starting point `init` writes
  into the user's own file, for them to edit.

Both start as comments. Put the defaults in the first:

```yaml
hello:
  greeting: Hello
  style: plain
```

Keep the `hello:` key at the top. Every command's defaults merge into one
document, and the order they merge in isn't fixed, so two commands that both
defined a top-level `timeout` would fight over it. Namespaced under the command,
they can't.

Rebuild and run:

```bash
just build
./bin/mytool hello
```

```
INFO  Hello
```

`style` isn't used yet. It's there for the validation step further down.

## Override from the environment

You set an environment prefix when you scaffolded, so every key can be set from
a variable: the prefix, then the key in capitals, with dots turned into
underscores.

```bash
MYTOOL_HELLO_GREETING="Hello from the environment" ./bin/mytool hello
```

```
INFO  Hello from the environment
```

You didn't register that variable anywhere. The prefix is what keeps it from
colliding with another tool's settings on the same machine.

One limit to know about: a key whose own name contains an underscore, such as
`update.check_interval`, is only reachable from the environment once something
below it declares the key. The [configuration reference](../reference/config/index.md)
explains why.

## Layer files with --config

Pass `--config` more than once and the files merge in order. Make two:

```bash
printf 'hello:\n  greeting: Team hello\n  style: loud\n' > config.yaml
printf 'hello:\n  greeting: Oi\n' > config.local.yaml
./bin/mytool hello --config ./config.yaml --config ./config.local.yaml
```

```
INFO  Oi
```

On a clash the later file wins, and a key that appears in only one file is kept.
That's why the team file still supplies `style: loud`.

But `--config` replaces the default locations rather than adding to them. Name
any file and `~/.mytool/config.yaml` drops out, so pass the whole stack you
want. A committed `config.yaml` with the team's settings and an untracked
`config.local.yaml` for your own is the usual pairing.

There's one more layer. A file named `.mytool.yaml` in the directory you run
from applies on top of your config files, so a repository can carry settings
for the tool. A project file can't set security-sensitive keys until the user
trusts it with `mytool config trust`.

Tidy up before moving on:

```bash
rm config.yaml config.local.yaml
```

## Refuse a bad value

Right now `style` can be anything. Make the command check its own settings. Add
`pkg/cmd/hello/config.go`:

```go
package hello

import "gitlab.com/phpboyscout/go/config"

// HelloConfig describes the config keys the hello command consumes.
type HelloConfig struct {
	Greeting string `config:"hello.greeting" validate:"required"`
	Style    string `config:"hello.style" enum:"plain,loud" default:"plain"`
}

// ValidateHelloConfig checks the hello config against its schema.
func ValidateHelloConfig(cfg config.Reader) error {
	return config.ValidateStruct[HelloConfig](cfg)
}
```

The tags carry the rules: `required` means the key must be present and
non-empty, and `enum` lists the values `style` accepts. The `default` tag only
documents the default in error messages. Real defaults live in
`assets/config.yaml`.

Call it at the top of `RunHello`, and use the style while you're there:

```go
package hello

import (
	"context"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func RunHello(_ context.Context, props *props.Props, _ *HelloOptions, _ []string) error {
	cfg := props.Config.View()

	if err := ValidateHelloConfig(cfg); err != nil {
		return err
	}

	greeting := cfg.GetString("hello.greeting")
	if cfg.GetString("hello.style") == "loud" {
		greeting = strings.ToUpper(greeting)
	}

	props.Logger.Info(greeting)

	return nil
}
```

Rebuild, then give it a style it doesn't know:

```bash
just build
MYTOOL_HELLO_STYLE=shout ./bin/mytool hello
```

```
ERROR config: configuration is not valid: config validation failed:
  hello.style: value "shout" is not allowed (hint: allowed values: plain, loud)
```

It exits 1 and names the key, the value, and what it would have accepted. Try a
value it does accept:

```bash
MYTOOL_HELLO_STYLE=loud ./bin/mytool hello
```

```
INFO  HELLO
```

Generating a new command with `--with-config-validation` writes this
`config.go` for you. See [Define and validate config for a component](../how-to/validate-component-config.md).

## Catch a misspelt key

A command's own validation can't catch a typo in a key name. If you write
`greting: Oi` in your config file, the real `greeting` still comes from the
defaults, the misspelt one is never read, and nothing complains.

The `config` feature can. It isn't on by default, so switch it on and rebuild:

```bash
gtb enable config
just build
```

Then misspell the key in `~/.mytool/config.yaml`:

```yaml
hello:
  greting: Oi
```

```bash
./bin/mytool config validate
```

```
warning: hello.greting: unknown configuration key (hint: check for typos; remove hello.greting if it is not needed)
configuration is valid
```

It's a warning, not a failure, and it doesn't run your command's own rules: an
unknown key is reported, but a bad `style` still only fails when `hello` runs.
Put the key back to `greeting` before carrying on.

## Ask the user for what you can't default

Some settings have no sensible default: a token, or an endpoint that differs
per person. For those, a command can bring its own step to `init`. Generate a
second command with an initialiser:

```bash
gtb generate command --name greet --short "Greet someone" --with-initializer
```

Alongside the usual files you get `pkg/cmd/greet/init.go`, generated and marked
`DO NOT EDIT`. It registers three things with `init`: the initialiser itself, a
`--skip-greet` flag, and an `init greet` subcommand for reconfiguring just this
command later. The initialiser counts as configured once the `greet` key exists,
so `init` asks only the first time.

The part that's yours is `InitGreet` in `pkg/cmd/greet/main.go`. Replace the
file with this:

```go
package greet

import (
	"context"

	"charm.land/huh/v2"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func RunGreet(_ context.Context, p *props.Props, _ *GreetOptions, _ []string) error {
	p.Logger.Info(p.Config.View().GetString("greet.greeting"))

	return nil
}

func InitGreet(ctx context.Context, p *props.Props, cfg setup.Editor) error {
	var greeting string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("What greeting should greet use?").
				Value(&greeting),
		),
	)
	if err := setup.RunForm(ctx, p, form); err != nil {
		return err
	}

	return cfg.Set("greet.greeting", greeting)
}
```

`setup.RunForm` runs the form on the command's own terminal. It honours
`--accessible`, and it refuses cleanly when nobody is there to answer rather
than hanging. `cfg.Set` writes straight into the user's config file.

Build it and run `init` again:

```bash
just build
./bin/mytool init --skip-login --skip-key
```

It stops at your question. Answer it, then:

```bash
./bin/mytool greet
```

```
INFO  Howdy
```

Run `init` a second time and it doesn't ask, because the key is set. To change
the answer, run `./bin/mytool init greet`. On a fresh machine,
`./bin/mytool init --skip-greet` leaves the question out.

## Where this leaves you

`hello` reads a real setting with a shipped default, takes overrides from the
environment and from layered files in a predictable order, and refuses a value
it doesn't understand. `greet` asks for its setting once, at `init`, and keeps
the answer.

## What to read next

- [Configure a tool's config stack](../how-to/configure-the-config-stack.md):
  other file formats, and sources such as Vault or Consul.
- [Add an initialiser](../how-to/add-initialiser.md): initialisers outside a
  generated command.
- [Configuration reference](../reference/config/index.md): every key, and the
  order the layers resolve in.
