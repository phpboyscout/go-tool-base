---
title: "Generic Config Validation Helper Specification"
description: "Add a generic ValidateStruct[T] helper to pkg/config so callers validate a tagged config struct against the container without building a schema by hand or type-asserting Props.Config to the concrete *config.Container."
date: 2026-05-29
status: DRAFT
tags:
  - specification
  - config
  - validation
  - generics
  - ergonomics
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.com
  - name: Claude (claude-opus-4-8)
    role: AI drafting assistant
---

# Generic Config Validation Helper Specification

Authors
:   Matt Cockayne, Claude (claude-opus-4-8) *(AI drafting assistant)*

Date
:   29 May 2026

Status
:   DRAFT

Builds on
:   [Config Schema Validation Specification](2026-03-26-config-schema-validation.md) (IMPLEMENTED)

---

## Overview

The schema validation layer added in the [config schema validation
spec](2026-03-26-config-schema-validation.md) works, but the call site it
produces is clunkier than it needs to be. Validating a command's config today
means building a schema, calling `Validate`, and unwrapping the result by
hand, and because the generated helper is typed against the concrete
`*config.Container`, the caller also has to type-assert `Props.Config` (which is
the `config.Containable` interface) down to the concrete type before it can call
the helper at all.

This specification proposes a small, generic addition to `pkg/config`,
`ValidateStruct[T]` (with a supporting `SchemaOf[T]`), that collapses the whole
dance into one call against the interface. No schema plumbing, no type
assertion, no per-command boilerplate function. It is a pure ergonomics and
safety change: the validation semantics defined by the original spec are
untouched.

---

## Motivation: the current shape

A command that validates its config slice currently carries a generated
`config.go` like this (`internal/generator/templates/command.go`,
`CommandConfigValidation`):

```go
func ValidateHelloConfig(cfg *config.Container) error {
	schema, err := config.NewSchema(config.WithStructSchema(HelloConfig{}))
	if err != nil {
		return err
	}

	result := cfg.Validate(schema)
	if !result.Valid() {
		return errors.New(result.Error())
	}

	return nil
}
```

And to call it, the command has to reach through the interface to the concrete
type (`docs/how-to/validate-component-config.md`, step 4):

```go
container, ok := p.Config.(*config.Container)
if !ok {
	return errors.New("config container required for validation")
}
if err := myfeature.ValidateConfig(container); err != nil {
	return err
}
```

Two problems:

1. **The type assertion is unnecessary and leaky.** `Validate(schema *Schema)
   *ValidationResult` is already a method on the `Containable` interface
   (`pkg/config/container.go`). The only reason the cast exists is that the
   generated helper chose to type its parameter as `*config.Container`. Asking
   every caller to assert down to a concrete type, and to invent an error for the
   "impossible" failure case, leaks an implementation detail and adds noise to
   every command that validates.

2. **The schema-building boilerplate repeats per command.** `NewSchema` +
   `WithStructSchema` + `Validate` + `Valid()`/`Error()` is the same five lines
   in every generated `config.go`. The type itself is the only thing that varies,
   which is exactly the shape a generic eliminates.

The project targets Go 1.26, so type parameters are available without
reservation.

---

## Proposed API

Two additions to `pkg/config`, both generic over the config struct type `T`:

```go
// SchemaOf returns a Schema derived from T's struct tags. The result is cached
// per type, so repeated calls for the same T do not re-reflect. Additional
// options (e.g. WithStrictMode) are applied on top and bypass the cache.
func SchemaOf[T any](opts ...SchemaOption) (*Schema, error)

// ValidateStruct validates cfg against the schema derived from T and returns a
// formatted error if any rule fails. It takes the Containable interface, so no
// type assertion to *Container is needed.
func ValidateStruct[T any](cfg Containable, opts ...SchemaOption) error
```

Reference implementation:

```go
func SchemaOf[T any](opts ...SchemaOption) (*Schema, error) {
	// Fast path: no extra options, cache by reflect type.
	if len(opts) == 0 {
		t := reflect.TypeFor[T]()
		if s, ok := schemaCache.Load(t); ok {
			return s.(*Schema), nil
		}
		schema, err := NewSchema(WithStructSchema(*new(T)))
		if err != nil {
			return nil, err
		}
		schemaCache.Store(t, schema)
		return schema, nil
	}
	// With caller-supplied options, build fresh (options may change the schema).
	return NewSchema(append([]SchemaOption{WithStructSchema(*new(T))}, opts...)...)
}

func ValidateStruct[T any](cfg Containable, opts ...SchemaOption) error {
	schema, err := SchemaOf[T](opts...)
	if err != nil {
		return err
	}

	if result := cfg.Validate(schema); !result.Valid() {
		return errors.New(result.Error())
	}

	return nil
}

var schemaCache sync.Map // reflect.Type -> *Schema
```

Both build on the existing, unchanged `NewSchema`, `WithStructSchema`,
`Containable.Validate`, and `ValidationResult`. Nothing in the validation
engine moves.

---

## Before and after

A command that opts into validation goes from this:

```go
// config.go
func ValidateHelloConfig(cfg *config.Container) error {
	schema, err := config.NewSchema(config.WithStructSchema(HelloConfig{}))
	if err != nil {
		return err
	}
	result := cfg.Validate(schema)
	if !result.Valid() {
		return errors.New(result.Error())
	}
	return nil
}

// main.go
container, ok := props.Config.(*config.Container)
if !ok {
	return errors.New("config container required for validation")
}
if err := ValidateHelloConfig(container); err != nil {
	return err
}
```

to this:

```go
// main.go
if err := config.ValidateStruct[HelloConfig](props.Config); err != nil {
	return err
}
```

The `HelloConfig` struct (the part that actually carries the per-command rules)
stays exactly as it is. Everything else is deleted.

Strict mode and other schema options pass straight through:

```go
config.ValidateStruct[HelloConfig](props.Config, config.WithStrictMode())
```

---

## Design decisions

- **Take `Containable`, not `*Container`.** This is the core of the fix. The
  validate method is on the interface; the helper should be too. The type
  assertion disappears from every call site.
- **A free function, not a method.** Go methods cannot take type parameters, so
  `ValidateStruct[T]` is a package-level function. This reads naturally
  (`config.ValidateStruct[HelloConfig](cfg)`) and keeps `T` at the call site
  where the struct is known.
- **`SchemaOf[T]` as a reusable building block.** Some callers will want the
  `*Schema` itself (for hot-reload re-validation, or to register with a
  watcher). Exposing the cached constructor separately avoids forcing them back
  onto raw `NewSchema`.
- **Cache only the option-free path.** A schema for a given `T` with no extra
  options is immutable, so caching it by `reflect.Type` is safe and removes
  per-call reflection. Calls that pass options build fresh, since options can
  change the resulting schema.
- **Semantics unchanged.** Required/enum/type checks, the unknown-key
  warning-vs-strict behaviour, and the `config validation failed:` error format
  are all inherited from the existing layer. This spec adds no new validation
  behaviour.

---

## Generator and docs changes

- **`CommandConfigValidation` template** (`internal/generator/templates/command.go`):
  stop emitting the hand-rolled `Validate<Name>Config(*config.Container)`
  function. Emit just the `<Name>Config` struct, and have the command call
  `config.ValidateStruct[<Name>Config](props.Config)` where it currently calls
  the generated function. (If a named per-command function is still wanted for
  discoverability, generate a one-line wrapper typed against `Containable`:
  `func Validate<Name>Config(cfg config.Containable) error { return config.ValidateStruct[<Name>Config](cfg) }`.)
- **How-to** (`docs/how-to/validate-component-config.md`): drop the
  `p.Config.(*config.Container)` assertion from step 4 and the later examples;
  show the one-line `ValidateStruct[T]` call.
- **Component reference** (`docs/components/config.md`): document `SchemaOf[T]`
  and `ValidateStruct[T]` alongside `NewSchema`/`WithStructSchema`.

---

## Backwards compatibility and migration

- **Non-breaking.** `NewSchema`, `WithStructSchema`, `Containable.Validate`, and
  any existing generated `Validate<Name>Config(*config.Container)` continue to
  compile and behave identically. The new helpers are additive.
- **Migration is opt-in.** Regenerating a command adopts the new form. Hand-written
  callers can swap the assertion and schema block for the single
  `ValidateStruct[T]` call at their own pace.
- A later release may deprecate generating the concrete-typed helper, once
  in-tree usages have moved over.

---

## Out of scope

- Any change to validation rules, tag vocabulary, or unknown-key handling.
- Auto-wiring validation into a command's lifecycle (it remains an explicit call
  the author makes; that is a separate question).
- Unmarshalling config into the struct. `T` is used only to derive the schema;
  values are still read with the typed getters.

---

## Open questions

1. **Naming.** `ValidateStruct[T]` is explicit; `Validate[T]` is shorter but sits
   awkwardly next to the existing `Container.Validate` method. `Check[T]` is
   another option. Preference?
2. **Keep a per-command function?** Inlining `ValidateStruct[T]` at the call site
   is the leanest, but a generated `Validate<Name>Config` (wrapping the generic)
   keeps a named, greppable entry point per command. Which does the generator
   emit?
3. **Cache eviction.** A `sync.Map` keyed by `reflect.Type` is effectively
   permanent. For the handful of config structs a tool defines this is a
   non-issue, but worth a note if that assumption ever changes.
