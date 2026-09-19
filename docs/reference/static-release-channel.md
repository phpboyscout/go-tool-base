---
title: "Static release channel layout"
description: "The exact layout a static release location must have for a GTB-built tool to discover and retrieve its releases: the pointer, the per-tag manifest, every field, the publish order and what a reader refuses."
date: 2026-09-19
tags: [reference, update, release, static-channel]
authors: [Matt Cockayne <matt@phpboyscout.com>]
---

# Static release channel layout

A tool on the static release channel (spec [0203](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0203-the-static-release-channel)) discovers and retrieves its releases from a plain HTTPS location with no forge involved. This page is the contract for that location: what must be there, named how, containing what, published in which order. Anything that produces this layout is a valid target, whether it is the scaffolded GoReleaser configuration, a hand-rolled uploader or another build system.

The examples use `https://pkg.example.com/acme/mytool` as the **base URL**, which is the one value a tool is configured with.

## Paths

| Object | Path | Mutable |
|---|---|---|
| Pointer | `<base>/latest.json` | Yes, the only one. Moved forward on each publish. |
| Release manifest | `<base>/<tag>/release.json` | No. Written once with the release. |
| Archives | `<base>/<tag>/<name>` | No. |
| Checksums | `<base>/<tag>/checksums.txt` | No. Written after every archive. |
| Signature | `<base>/<tag>/checksums.txt.sig` | No. Present only for a signed release. |

`<tag>` is the release's tag as the tool reports it, `v1.2.0`, with the `v`. Everything for one release sits under its tag; nothing is shared between tags. The location needs no directory listing: a reader never lists, it fetches the pointer and follows what the documents say.

## The pointer: `latest.json`

```json
{
  "schema": 1,
  "tool": "mytool",
  "tag": "v1.2.0",
  "manifest": "https://pkg.example.com/acme/mytool/v1.2.0/release.json",
  "published_at": "2026-09-19T12:20:00Z"
}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `schema` | integer | yes | Document schema. This page describes `1`. A reader refuses a value it does not know. |
| `tool` | string | yes | The tool's name, as `project_name` in GoReleaser. |
| `tag` | string | yes | The current release's tag, semver with an optional `v`. |
| `manifest` | string | yes | Absolute URL of that release's `release.json`. Must be under the base URL. |
| `published_at` | string | yes | RFC 3339 timestamp of the publish that moved the pointer. |

## The release manifest: `<tag>/release.json`

```json
{
  "schema": 1,
  "tool": "mytool",
  "tag": "v1.2.0",
  "released_at": "2026-09-19T12:17:26Z",
  "previous": "v1.1.0",
  "checksums": "https://pkg.example.com/acme/mytool/v1.2.0/checksums.txt",
  "signature": "https://pkg.example.com/acme/mytool/v1.2.0/checksums.txt.sig",
  "notes": "",
  "downloads": [
    {
      "os": "linux",
      "arch": "amd64",
      "name": "mytool_Linux_x86_64.tar.gz",
      "url": "https://pkg.example.com/acme/mytool/v1.2.0/mytool_Linux_x86_64.tar.gz",
      "size": 23456789,
      "sha256": "ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81"
    }
  ]
}
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `schema` | integer | yes | Document schema, `1`. |
| `tool` | string | yes | The tool's name. |
| `tag` | string | yes | This release's tag. |
| `released_at` | string | yes | RFC 3339 timestamp of the build. |
| `previous` | string | yes, may be empty | The tag of the release published before this one, or `""` for the first. A tag, never a URL: the chain cannot leave the base. |
| `checksums` | string | yes | Absolute URL of `checksums.txt`, under the base. |
| `signature` | string | no | Absolute URL of the detached signature over `checksums.txt`, under the base. Omit the key for an unsigned release. |
| `notes` | string | no | Release notes as text. Omit or leave empty when there are none. |
| `downloads` | array | yes, at least one | One entry per archive. |

Each download:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `os` | string | yes | Go's `GOOS`: `linux`, `darwin`, `windows`, … |
| `arch` | string | yes | Go's `GOARCH`: `amd64`, `arm64`, … |
| `name` | string | yes | The archive's file name, as it appears in `checksums.txt`. |
| `url` | string | yes | Absolute URL of the archive, under the base. |
| `size` | integer | yes | Size in bytes, greater than zero. |
| `sha256` | string | yes | Lowercase hex SHA-256 of the archive, 64 characters, the same digest `checksums.txt` carries. |

Downloads are ordered by `os` then `arch`, so the document is stable whatever order the build ran in.

## Discovery and retrieval, from the reader's side

1. Fetch `<base>/latest.json`. Refuse an unknown `schema`; refuse a `manifest` that is not under the base.
2. Fetch the manifest it names; validate it the same way.
3. Pick the download whose `os` and `arch` match the running binary. None means this release does not ship for the platform.
4. Fetch `checksums.txt` and, when named, `checksums.txt.sig`; verify the signature against the trust set and the archive against the checksum exactly as on the forge channel. The manifest's `sha256` lets a reader refuse a mismatched archive before it ever reaches the verifier.
5. To list versions, follow `previous` from manifest to manifest. To pin a version, fetch `<base>/<tag>/release.json` directly.

A reader trusts nothing in a document that would take it off the base: every URL must share the base URL's scheme and host and sit under its path. `https` cannot be walked down to `http`.

## Publishing, in order

The order is what keeps a reader safe at every instant:

1. Upload every archive.
2. Upload `checksums.txt` (and `checksums.txt.sig` when signing). On the estate's store this is the completion sentinel: a tag with `checksums.txt` is a finished publish.
3. Upload `<tag>/release.json`.
4. Only then move `<base>/latest.json` to name the new tag, with a conditional write (`If-Match` on the ETag read at the start of the publish) so two publishes cannot silently clobber each other.

A pointer that is behind (a publish that died before step 4) offers nobody a release the location does not fully hold, which is safe, and is fixed by re-running step 4. A pointer that is ahead is impossible in this order.

Nothing under `<tag>/` is ever modified or removed once published. The pointer is the one object that changes.

## Producing the documents with GoReleaser

The framework ships a tool that writes both documents from a GoReleaser `dist/`, and a project generated with `--release-channel static` gets the three pieces below in its `.goreleaser.yaml` (gtb's own carries the same). They need GoReleaser **Pro**: the manifest has to be written after `checksums.txt` and its signature exist and before anything is uploaded, and only Pro's `before_publish` hooks run in that slot (GoReleaser's `artifacts.json` is written after publishing, in both editions, so the tool does not read it).

```yaml
before_publish:
  - cmd: go tool releasemanifest --dist dist --base-url https://pkg.example.com/acme/mytool
    artifacts: [checksum]   # the checksum artifact is one object, so the hook runs once

blobs:
  - provider: s3
    bucket: "{{ .Env.RELEASE_STORE_BUCKET }}"
    endpoint: "{{ .Env.RELEASE_STORE_ENDPOINT }}"
    directory: "acme/mytool/{{ .Tag }}"   # the base URL's path, then the tag
    extra_files:
      - glob: dist/release.json

publishers:
  - name: latest-pointer
    checksum: true
    ids: [latest-pointer]   # no archive carries this id; the checksum artifact belongs to every id, so this runs once
    env:                    # a publisher runs with a scrubbed environment; the store credentials are passed through
      - AWS_ACCESS_KEY_ID={{ .Env.AWS_ACCESS_KEY_ID }}
      - AWS_SECRET_ACCESS_KEY={{ .Env.AWS_SECRET_ACCESS_KEY }}
    cmd: bash scripts/move-pointer.sh dist/latest.json {{ .Env.RELEASE_STORE_ENDPOINT }}/{{ .Env.RELEASE_STORE_BUCKET }}/acme/mytool/latest.json
```

1. **`releasemanifest`** (`go tool releasemanifest`, declared in the framework's `go.mod` `tool` block) reads `dist/metadata.json` for the tool name, tag and date, `dist/checksums.txt` for the archives and their digests, and each archive itself: its size from the file and its `os` and `arch` from the build info of the Go binary inside, so the archive's name never matters. It reads the current pointer at the base URL to fill `previous` (empty when there is none yet) and writes `dist/release.json` and `dist/latest.json`. It refuses a `dist/` with an archive that `checksums.txt` does not list, two archives for one platform, or an archive holding no Go binary. `--previous <tag>` overrides the pointer lookup; `--notes <file>` supplies the notes.
2. **`blobs:`** uploads `release.json` with the archives, checksums and signature, immutable like them.
3. **`publishers:`** runs last in the publish pipe (and not at all when publishing is skipped) and moves the pointer. `scripts/move-pointer.sh` (scaffolded with the channel; the framework repository carries the same file) is the conditional write of the publish order above: given the pointer file and the object's URL on the store's S3 endpoint, it reads the pointer's ETag with a signed `HEAD`, then `PUT`s `dist/latest.json` with `If-Match` on it (`If-None-Match: *` for a first release) and `Cache-Control: no-cache`, using `curl`'s own SigV4 signing (`AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`, the same credentials as `blobs:`). A 412 fails the job and names the condition that failed.

The pipeline supplies `GORELEASER_KEY`, `RELEASE_STORE_ENDPOINT`, `RELEASE_STORE_BUCKET` and the store credentials; the public base URL must serve the bucket's path prefix. [Secure releases](../how-to/secure-releases.md#publishing-on-the-static-channel) is the how-to.

A publisher without GoReleaser writes both documents from this page and follows the order above.

## Related

- [Configure self-updating](../how-to/configure-self-updating.md): choosing the channel for a generated tool.
- [Secure releases](../how-to/secure-releases.md): checksums, signatures and the trust set the reader verifies against.
- [Spec 0203](https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0203-the-static-release-channel): the decisions behind the layout.
