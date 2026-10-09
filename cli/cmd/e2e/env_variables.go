package main

import "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

// The scenario variables the e2e binary reads by name, declared so doctor
// scenarios do not report them as stray (spec 0205 D4).
func init() {
	for _, name := range []string{configSourceEnv, releaseScenarioEnv, staticChannelEnv} {
		setup.DeclareEnvVariable(name)
	}
}
