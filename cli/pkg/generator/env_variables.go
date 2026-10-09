package generator

import "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

// NonInteractiveEnv set to "true" makes the generator take every default
// rather than prompt.
const NonInteractiveEnv = "GTB_NON_INTERACTIVE"

// The generator reads these by name. Under gtb's own prefix they also map to
// config keys, so declaring them keeps doctor from calling them stray (spec
// 0205 D4).
func init() {
	for _, name := range []string{NonInteractiveEnv, SkipLintEnv, FrameworkReplaceEnv} {
		setup.DeclareEnvVariable(name)
	}
}
