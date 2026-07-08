package bitbucket

import "gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"

type testReleaseConfig struct {
	values map[string]string
	subs   map[string]testReleaseConfig
}

func (c testReleaseConfig) GetString(key string) string {
	return c.values[key]
}

func (c testReleaseConfig) Sub(key string) release.Config {
	sub, ok := c.subs[key]
	if !ok {
		return nil
	}

	return sub
}

func bitbucketConfig(values map[string]string) release.Config {
	return testReleaseConfig{subs: map[string]testReleaseConfig{
		"bitbucket": {values: values},
	}}
}
