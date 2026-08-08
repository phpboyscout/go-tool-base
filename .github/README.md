# go-tool-base

**The application lifecycle framework for Go command-line tools and services.**
Config, errors, transport, observability and release plumbing that already agree
with each other, so a new tool starts with the boilerplate already wired.

> **This is a read-only mirror. The canonical repository is on GitLab:**
> **https://gitlab.com/phpboyscout/go-tool-base**
>
> Issues and merge requests are handled there.

## Installing

The module path is the GitLab one:

```
go get gitlab.com/phpboyscout/go-tool-base
```

`go get github.com/phpboyscout/go-tool-base` will not work. The mirror is here
for browsing and reference only.

Much of the framework is also published as small, independently versioned modules
that work without it: see **https://go.phpboyscout.uk** for the full set.

## Documentation

Full documentation: **https://gtb.phpboyscout.uk**

Guides built on it, each a curated route rather than a tag archive:

- [Building a command-line tool in Go](https://phpboyscout.uk/topics/building-a-cli-in-go/) — a five-part tutorial and the decisions underneath it
- [Building a web service in Go](https://phpboyscout.uk/topics/building-a-web-service-in-go/) — gRPC, REST and a generated gateway
- [Signing your releases](https://phpboyscout.uk/topics/signing/) — proving the binary came from you
