---
title: "Testing huh / charm interactive forms"
description: "How to unit-test code that drives charm.land/huh forms without a TTY: using huh's built-in accessible mode with scripted stdin, injecting the form creator, or driving the form as a Bubble Tea model. Includes copy-paste helpers and a decision guide."
tags: [testing, development, huh, charm, tui]
authors: [Matt Cockayne <matt@phpboyscout.uk>]
---

# Testing huh / charm interactive forms

Interactive prompts in GTB are built on [`charm.land/huh`](https://github.com/charmbracelet/huh). A form's `Run()` method takes over the terminal and blocks on real keyboard input, so a naive unit test either **hangs forever** (no TTY to read) or can't assert anything. This page covers the three ways to test form-driving code headlessly, when to reach for each, and the sharp edges.

> This is the same class of problem behind the `gtb init` hang fix (an unguarded wizard blocking on non-TTY stdin) and the `config migrate-credentials` coverage work. See [the migrate-wizard spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0106-config-migrate-wizard-injectability).

## TL;DR: pick an approach

| Situation | Approach | Parallel-safe? |
|-----------|----------|----------------|
| Code runs its form through `setup.RunForm(ctx, p, form)` (every form in `pkg/`) | **D. `Props.IO` with `internal/formtest`** | Yes |
| You need to assert **field-level keystroke behaviour** (a hide function, a dynamic select) | **C. Drive the form as a `tea.Model`**, or D with `formtest.Keys` | Yes |
| Legacy code calls `huh.NewForm(...).Run()` directly and you can't change it | **A. Accessible mode + scripted stdin** (`TERM=dumb`, swap `os.Stdin`) | No (serial) |
| The field is a **password** (`EchoMode(huh.EchoModePassword)`) | D with `formtest.Keys`, or C (accessible mode can't script it, see [gotchas](#gotchas)) | — |

The default for framework code is **D**. Injecting a form creator (the old
"B, `WithForm` pattern") is gone: it left the wizard's own forms untested and
put test-only options in the public API (spec 0198).

## D. `Props.IO` and `internal/formtest`

`Props.IO` is the invocation's streams (`props.StdIO{}` is the process's).
`setup.RunForm` runs every form on them, applies the IO's accessible decision,
and refuses a stdin that is neither a terminal nor accessible before the form
opens. A test sets the IO and drives the real form:

```go
p := &props.Props{IO: props.StdIO{
    Stdin:          formtest.Answers("2", "MY_VAR", "y"), // one answer per field
    Stdout:         io.Discard,
    Stderr:         io.Discard,
    AccessibleMode: true,
}}
require.NoError(t, RunAIInit(ctx, p, dir))
```

`formtest.Answers` is what a person types at accessible prompts: an option's
number for a select, a line for an input, `y`/`n` for a confirm. It yields one
answer per `Read`, because huh reads each field through a fresh buffered
reader and a plain `strings.Reader` would lose every answer after the first.

For behaviour that only the TUI path has (a hide function, `OptionsFunc`, a
password field), drive keys instead:

```go
p := &props.Props{IO: formtest.TUI(formtest.Keys(formtest.Down, formtest.Enter, "MY_VAR", formtest.Enter))}
```

`Keys` paces one sequence per `Read` (the parser merges bytes that arrive
together) and `TUI` is an IO that reports interactive so the form runs
headless with no renderer. Slower (tens of milliseconds a key), so reach for it
only when the accessible route cannot express the behaviour.

Three facts about accessible mode decide which route a test takes (huh
v2.0.3, `form.go` `runAccessible`):

- **Every field of every group is asked**, in order. `WithHideFunc` is not
  consulted, so an answers script covers the hidden pages too, and a test that
  a page *is* hidden has to drive keys.
- **Group titles and descriptions are not printed.** Assert on field titles.
- **A field's error is swallowed.** A password input with no terminal behind
  it fails with "password asking needs a tty" and the bound value stays blank.
  A wizard that must have the value checks for the blank itself
  (`promptManualToken` returns `ErrNoTokenEntered`).

The answers route touches nothing global and is parallel-safe. The key route
is time-paced (huh's group transitions are asynchronous commands), so tests
that drive keys do not call `t.Parallel()`.

## How huh makes this possible: accessible mode

huh ships a first-class **accessible mode** (built for screen readers) that replaces the full-screen TUI with plain line-based prompts. Two facts make it the key to headless testing:

1. **It auto-enables when `TERM=dumb`.** In `form.go`, `NewForm` calls `WithAccessible(true)` when `os.Getenv("TERM") == "dumb"`. No code change required to flip it on, just the env var.
2. **It reads from `os.Stdin` by default.** `Form.RunWithContext` dispatches to `runAccessible(output|os.Stdout, input|os.Stdin)`. Each field's `RunAccessible(w, r)` does a simple line read from `r`.

So setting `TERM=dumb` and feeding `os.Stdin` drives the **real** production form, no stubbing, no refactor.

### Accessible input formats per field

What you write to stdin depends on the field type:

| Field | Reads | Feed | Empty line |
|-------|-------|------|------------|
| `huh.NewInput()` (normal) | one line (`PromptString`) | `"MY_VALUE\n"` | uses the field's default value |
| `huh.NewConfirm()` | `y`/`n` (`PromptBool`) | `"y\n"` or `"n\n"` | uses the default (`[Y/n]` vs `[y/N]`) |
| `huh.NewSelect()` | a 1-based option number (`PromptInt`) | `"2\n"` | uses the default option |
| `huh.NewText()` | one line | `"some text\n"` | default |
| `huh.NewNote()` | nothing (display only) | — | — |
| `huh.NewInput().EchoMode(huh.EchoModePassword)` | raw terminal fd (`PromptPassword`) | **not scriptable via a pipe** | — |

Invalid input (a failing `Validate`) **re-prompts**: the field loops and consumes another line. Always feed a value that passes validation, or the test will block waiting for the next line.

## Approach A: accessible mode + scripted stdin (recommended for existing code)

Drop this helper into your `_test.go` (it lives in `pkg/cmd/config/migrate_forms_test.go` for the migrate wizard):

```go
// withScriptedStdin runs fn with huh in accessible mode (TERM=dumb) and os.Stdin
// fed from script, restoring both afterwards. Tests using it must NOT call
// t.Parallel(): it mutates process-global os.Stdin/os.Stdout and TERM.
func withScriptedStdin(t *testing.T, script string, fn func()) {
	t.Helper()
	t.Setenv("TERM", "dumb") // huh → accessible mode; also forbids t.Parallel

	r, w, err := os.Pipe()
	require.NoError(t, err)

	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin = r

	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stdout = devnull // swallow the accessible prompt text

	t.Cleanup(func() {
		os.Stdin, os.Stdout = origIn, origOut
		_ = devnull.Close()
	})

	go func() {
		_, _ = io.WriteString(w, script)
		_ = w.Close()
	}()

	fn()
}
```

### Example: an input prompt

```go
func TestResolveEnvVarName_InteractivePrompt(t *testing.T) {
	withScriptedStdin(t, "MY_CUSTOM_TOKEN\n", func() {
		name, err := resolveEnvVarName(MigrateOptions{}, literalCredential{Key: "github.auth.value"})
		require.NoError(t, err)
		assert.Equal(t, "MY_CUSTOM_TOKEN", name)
	})
}
```

### Example: a confirm prompt, plus a real side effect

```go
func TestInstructAndVerifyEnvVar_ConfirmedButUnset(t *testing.T) {
	withScriptedStdin(t, "y\n", func() {
		// Var deliberately not exported → the post-confirm verification fails.
		err := instructAndVerifyEnvVar("UNSET_TOKEN", literalCredential{Key: "github.auth.value"}, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not set in the current environment")
	})
}
```

### Why these tests must be serial

`withScriptedStdin` swaps **process-global** `os.Stdin`/`os.Stdout` and sets `TERM`. `t.Setenv` deliberately panics if `t.Parallel()` was called, which is the safety interlock: these tests run in `go test`'s sequential phase, where the package's parallel tests are paused, so nothing else reads `os.Stdin` concurrently. This is a contained, test-only use of global state (**not** a production mocking hook) so it does not violate the [no-package-level-hooks rule](../../how-to/testing.md#no-package-level-mocking-hooks). See also [`t.Parallel()` + `t.Setenv()` are incompatible](../../how-to/testing.md#tparallel-tsetenv-are-incompatible).

## Approach B: inject the form (best for new code)

When you control the code, make the form **creator** injectable so tests supply a deterministic one. This is parallel-safe (no globals) and the established pattern in `pkg/setup/forge` (both the single-token `WithAuthForm` and the dual-credential `WithDualForm` seams) and `pkg/setup/ai`.

```go
// Production seam: a creator func, defaulted to the real form, overridable in tests.
type DualFormOption func(*dualFormConfig)

func WithDualForm(creator func(*DualConfig) []*huh.Form) DualFormOption { /* ... */ }

// Test: inject a form pre-seeded with values, or one wired to scripted input.
i := forge.NewBitbucketInitialiser(p, forge.WithDualForms(
	forge.WithDualForm(func(cfg *forge.DualConfig) []*huh.Form {
		cfg.StorageMode = credentials.ModeEnvVar // set the outcome directly
		return []*huh.Form{huh.NewForm( /* trivial / no-op group */ )}
	}),
))
```

Prefer this for **new** interactive features. It keeps tests parallel and avoids the global-stdin dance. Retrofitting it onto existing code is only worth it when Approach A's serial constraint actually hurts (it rarely does for a handful of wizard tests).

## Approach C: drive the form as a `tea.Model`

Every `huh.Form` is a Bubble Tea model. If your code hands you the `*huh.Form` (rather than calling `.Run()` itself), you can feed synthetic key events and assert on `form.State`: fully parallel, no global state. This is how huh tests itself.

```go
// Minimal key-event helpers (huh keeps these unexported; copy them into your test).
func keypress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: string(r), Code: r, ShiftedCode: r})
}
func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: code}) }

func TestMyForm(t *testing.T) {
	t.Parallel()

	var name string
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Value(&name),
	))
	form.Update(form.Init())

	m, _ := form.Update(keypress('g'))
	m, _ = m.Update(keypress('t'))
	m, _ = m.Update(keypress('b'))
	m, _ = m.Update(key(tea.KeyEnter)) // submit

	assert.Equal(t, huh.StateCompleted, m.(*huh.Form).State)
	assert.Equal(t, "gtb", name)
}
```

Use this when you specifically need to assert **keystroke-level** behaviour (navigation, filtering, validation feedback) rather than just the final value.

## Gotchas

- **Password fields aren't scriptable via Approach A.** `EchoMode(huh.EchoModePassword)` routes through `PromptPassword`, which reads the **raw terminal fd** (it type-asserts `r.(interface{ Fd() uintptr })` and puts it in raw mode). A plain `os.Pipe` won't behave. Test secret entry with Approach B (set the value directly) or C.
- **Feed enough lines, then close the writer.** A form with N fields reads N lines. Under-feeding leaves the read blocking. The helper closes the pipe writer after writing, so a stuck read surfaces as a fast EOF rather than a hang.
- **Validation loops consume extra lines.** If a value fails `Validate`, the field re-prompts and reads again. Feed values that pass, or script the retry explicitly.
- **Redirect `os.Stdout`.** Accessible prompts print to stdout; without the `devnull` swap they spam the test log. (Note huh writes the prompt to `output|os.Stdout`, not stderr.)
- **Always restore globals.** The helper restores `os.Stdin`/`os.Stdout` via `t.Cleanup`; `t.Setenv` restores `TERM`. Never leave them swapped: later tests in the sequential phase would inherit them.
- **`TERM=dumb` only affects huh.** It does not change your code's behaviour; it only flips huh's renderer to the line-based accessible path.

## See also

- [Testing & Mocking](../../how-to/testing.md): the general unit-testing guide, race-avoidance rules, and `internal/exectest` fakes.
- [`pkg/cmd/config/migrate_forms_test.go`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/cmd/config/migrate_forms_test.go): the real `withScriptedStdin` tests for the migrate wizard.
- [`pkg/setup/forge/dual.go`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/setup/forge/dual.go): the `WithDualForm` injection pattern (Approach B); see [`single.go`](https://gitlab.com/phpboyscout/go-tool-base/-/blob/main/pkg/setup/forge/single.go) for the single-token `WithAuthForm` equivalent.
- [config migrate-wizard coverage spec](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0106-config-migrate-wizard-injectability): the decision record behind choosing accessible mode over a seam refactor.
