package config

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestRecognisedConfigKey(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{"myapp.setting": true}

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"framework section", "server.grpc.reflection", true},
		{"framework section credential", "anthropic.api.key", true},
		{"top-level framework key", "log.level", true},
		{"tool-declared key", "myapp.setting", true},
		{"unrecognised section", "weirdsection.typo", false},
		{"empty key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, recognisedConfigKey(tt.key, declared))
		})
	}
}

func TestToolDeclaredKeys_FlattensAssets(t *testing.T) {
	t.Parallel()

	// A tool that declares a custom key in its defaults asset.
	p := &props.Props{Assets: props.NewAssets(props.AssetMap{
		"tool": fstest.MapFS{
			"assets/config.yaml": &fstest.MapFile{Data: []byte("myapp:\n  feature:\n    enabled: true\n")},
		},
	})}

	keys := toolDeclaredKeys(p)
	assert.True(t, keys["myapp.feature.enabled"], "leaf key must be declared")
	assert.True(t, keys["myapp.feature"], "ancestor path must be declared")
	assert.True(t, keys["myapp"], "top-level section must be declared")
	assert.False(t, keys["other.key"])
}

func TestFlattenConfigKeys(t *testing.T) {
	t.Parallel()

	out := map[string]bool{}
	flattenConfigKeys(map[string]any{
		"a": map[string]any{"b": 1, "c": map[string]any{"d": 2}},
		"e": "leaf",
	}, "", out)

	assert.Equal(t, map[string]bool{
		"a": true, "a.b": true, "a.c": true, "a.c.d": true, "e": true,
	}, out)
}
