// Package vcs holds the thin configuration adapters that bridge GTB's resolved
// configuration to the extracted version-control modules.
//
// The forge backends themselves no longer live here. Release querying and the
// provider registry are the standalone module gitlab.com/phpboyscout/go/forge
// (with per-provider adapters forge-github, forge-gitlab, forge-gitea,
// forge-bitbucket, plus the built-in direct source); repository cloning and
// metadata are gitlab.com/phpboyscout/go/repo. What remains in this package is
// GTB-side glue:
//
//   - ConfigFromReader adapts a GTB config.Reader to the narrow forge.Config seam
//     the provider factories consume.
//   - the repo sub-package's config adapter maps GTB props/config to go/repo
//     Settings.
//
// These adapters stay in GTB because they encode GTB's config-key layout and
// credential-resolution conventions, which are not the modules' concern.
package vcs
