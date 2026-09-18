# VCS

GTB's config adapters for the standalone `gitlab.com/phpboyscout/go/forge` (release
providers, credential chain) and `gitlab.com/phpboyscout/go/repo` (git operations)
modules. The forge clients themselves (GitHub, GitLab, Gitea/Codeberg, Bitbucket)
live in the external `go/forge-<name>` modules, blank-imported by the binary that
ships them.

## Package structure

- **[repo](./repo)**: props/config adapters for the `go/repo` module.
- **config_adapter.go**: adapts a GTB config view to the `forge.Config` seam.
- **credential.go**: composes GTB's forge credential chain (`auth.env` →
  `auth.keychain` → `auth.value` → the well-known fallback env var) and hands it to
  a provider factory through `forge.WithCredential`.

For the full picture, see the
**[VCS component documentation](../../docs/explanation/components/vcs/index.md)**.
