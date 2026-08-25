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

## Why the validation exists (threat model)

Handing a URL to the OS opener is handing it to whatever handler the platform
has registered for that scheme, and the OS will happily execute **dangerous
schemes**. A `file://` URL opens a local file (a local-file-disclosure vector),
`javascript:` runs script in the launched browser, and `data:` can carry an
inline payload (a script/exfiltration vector); custom protocol handlers extend
the blast radius further. That is why `OpenURL` enforces a **scheme allowlist**
of `https`, `http`, and `mailto` and rejects everything else, alongside a length
bound (URLs above 8 KiB, below every supported platform's command-line limit)
and rejection of ASCII control characters and NUL bytes. Both of which can
smuggle a second argument or command past a platform URL handler.

The allowlist is deliberately **non-configurable**. There is no option to widen
the permitted schemes, because a configurable allowlist would become the exact
thing an attacker targets: a downstream tool tricked (via a config file, an
environment value, or a crafted release asset) into re-enabling `file://` or
`javascript:` would silently undo the whole control. A single hard-coded set
means every tool built on GTB inherits the same guarantee, and no configuration
surface can downgrade it.

`OpenURL` validates only the scheme and the URL's overall shape. It **cannot**
detect header-injection in `mailto:` URLs (an attacker-supplied `cc=`, `bcc=`,
or `body=`), which is why callers constructing `mailto:` from user-influenced
data must `url.QueryEscape` every parameter themselves. It is also not
context-aware once the OS spawns the handler process; the context it takes only
governs pre-open cancellation.

## Related

- **Module docs:** [browser.go.phpboyscout.uk](https://browser.go.phpboyscout.uk)
- **Trust model / spec:** [`0057-url-scheme-validation`](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0057-url-scheme-validation)
