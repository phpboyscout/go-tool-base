// Package setup provides self-update and bootstrap functionality for GTB-based
// tools. This file registers only `direct`, the plain download source that
// ships inside the forge module itself. Forge adapters (github, gitlab, gitea,
// bitbucket) are registered by the binary that ships them (spec 0194 D3):
// gtb's own main links every one, and a generated tool links the ones its
// enabled forge features need. forge.ModuleFor names the module for each.
package setup

import (
	_ "gitlab.com/phpboyscout/go/forge/direct"
)
