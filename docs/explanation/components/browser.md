---
title: "Browser, Safe URL Opening"
description: "go-tool-base consumes the standalone go/browser module for validated URL-opening (scheme allowlist, length bound, control-character rejection)."
date: 2026-07-13
tags: [component, security, browser, url]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Browser: Safe URL Opening

The safe URL-opening helper has been **extracted into the standalone
[`gitlab.com/phpboyscout/go/browser`](https://gitlab.com/phpboyscout/go/browser)
module**. Its full documentation, the `OpenURL` API, the `WithOpener` seam, the
scheme allowlist / length bound / control-character rejection, and the threat
model. Now lives at:

> **[browser.go.phpboyscout.uk](https://browser.go.phpboyscout.uk)**

`browser` is framework-free, so go-tool-base consumes it **directly** (no
adapter): callers import `gitlab.com/phpboyscout/go/browser` and use
`browser.OpenURL` as before. See the
[migration note](../../reference/migration/v0.x-browser-extracted.md) for the
import-path change.

## How go-tool-base uses it

All URL-opening in GTB (and in tools built on GTB) routes through
`browser.OpenURL`: the telemetry deletion-request `mailto:` flow, the GitHub
device-login browser hand-off, and the docs-server launch. Never call the OS
opener (`exec.Command("open"|"xdg-open"|"rundll32")` or `cli/browser`) directly.
Callers building `mailto:` URLs from user-influenced data must additionally
`url.QueryEscape` every parameter value.

## Why the validation exists

`OpenURL` enforces a non-configurable scheme allowlist (`https`, `http`,
`mailto`), a length bound, and control-character rejection, because handing a
URL to the OS opener hands it to whatever handler the platform registers for
that scheme (`file://`, `javascript:`, `data:`, and custom protocol handlers
are all live risks). `OpenURL` validates only the scheme and the URL's shape,
it **cannot** detect header-injection in `mailto:` URLs, which is why a GTB
caller building one from user-influenced data must `url.QueryEscape` every
parameter itself. The full threat model, including why the allowlist is
non-configurable, is on the module's own
[explanation page](https://browser.go.phpboyscout.uk/explanation/threat-model/).

## Related

- **Module docs:** [browser.go.phpboyscout.uk](https://browser.go.phpboyscout.uk)
- **Trust model / spec:** [`0057-url-scheme-validation`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0057-url-scheme-validation)
