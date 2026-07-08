---
name: verify-frontend-headless
description: Verify web-UI work on a headless server by driving chrome-headless-shell with puppeteer — build, serve, navigate, screenshot and drive the real page — instead of guessing or reaching for a GUI browser. Use to confirm any front-end or visual change actually works.
---

# Verify front-end work headlessly

This is a **headless development server — no desktop environment, no display.** Any
browser-based check (a web UI, a Hugo site, a studio app, a screenshot, a click/drag
interaction) must be driven **headlessly via puppeteer**, never a GUI browser and never by
reasoning about the markup. Build the app, run its server, drive the real page, screenshot it.

## The browser

A puppeteer-managed `chrome-headless-shell` lives under `~/.cache/puppeteer` (install with
`npx @puppeteer/browsers install chrome-headless-shell` if it's missing). Drive it with
**`puppeteer-core`**:

```js
const browser = await puppeteer.launch({
  executablePath: "<path to chrome-headless-shell>",
  headless: true,
  args: ["--no-sandbox", "--disable-dev-shm-usage"],
});
```

## The flow

1. **Build + serve** the app (start its dev/preview server in the background; for the blog
   that's `hugo server`).
2. **Wait until it's actually up** before navigating (poll the URL, don't race the server).
3. `page.goto(url)`, wait for the target selector or network idle.
4. **Drive the change, don't just load it** — click/scroll/fill the interaction the change
   affects, and `page.screenshot({ path })` before and after so you can see it worked.
5. **Test responsive** where it matters — set a mobile viewport and a desktop one and shoot both.
6. Read the screenshot back to confirm the result.
7. **Tear the server down** when finished.

## Why

Front-end work verified by reading the code is a guess. This is how you *see* it — the real
page, the real render, the real interaction — which is the only way to confirm a visual change
on a machine with no screen. Reach for it whenever a change has a visual surface.
