// Package setup provides self-update and bootstrap functionality for GTB-based
// tools. This file registers all built-in release providers via blank imports so
// that they are available whenever pkg/setup is imported.
package setup

import (
	_ "github.com/phpboyscout/go-tool-base/pkg/vcs/bitbucket"
	_ "github.com/phpboyscout/go-tool-base/pkg/vcs/direct"
	_ "github.com/phpboyscout/go-tool-base/pkg/vcs/gitea"
	_ "github.com/phpboyscout/go-tool-base/pkg/vcs/github"
	_ "github.com/phpboyscout/go-tool-base/pkg/vcs/gitlab"
)
