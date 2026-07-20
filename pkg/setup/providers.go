// Package setup provides self-update and bootstrap functionality for GTB-based
// tools. This file registers the release providers via blank imports so they
// are available whenever pkg/setup is imported.
//
// Each provider now ships as its own module, so a tool that needs only one
// forge can drop the others: replace this file's effect by importing just the
// providers you want before calling into setup. The full set is imported here
// because the framework cannot know which forge a downstream tool targets.
//
// `direct` is not a forge — it is a plain download source — and ships inside
// the forge module itself rather than separately.
package setup

import (
	_ "gitlab.com/phpboyscout/go/forge-bitbucket"
	_ "gitlab.com/phpboyscout/go/forge-gitea"
	_ "gitlab.com/phpboyscout/go/forge-github"
	_ "gitlab.com/phpboyscout/go/forge-gitlab"
	_ "gitlab.com/phpboyscout/go/forge/direct"
)
