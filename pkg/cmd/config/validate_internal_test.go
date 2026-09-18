package config

import (
	"testing"
	"testing/fstest"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"

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
		{"a credential root the registry declares", "azure.api.env", true},
		{"a dynamic feature flag", "features.telemetry.enabled", true},
		{"top-level framework key", "log.level", true},
		{"tool-declared key", "myapp.setting", true},
		{"unrecognised section", "weirdsection.typo", false},
		{"empty key", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, recognisedConfigKey(tt.key, declared, testFrameworkSections()))
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

// testFrameworkSections is frameworkSections over a registry that declares
// two chat credentials the way pkg/chat does in a binary that links them,
// so the test states what is declared rather than depending on the process
// registry.
func testFrameworkSections() map[string]bool {
	r := features.NewRegistry()
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "chat:anthropic", Label: "Anthropic API key",
		EnvKey: "anthropic.api.env", KeychainKey: "anthropic.api.keychain", LiteralKey: "anthropic.api.key"})
	credentialposture.RegisterOn(r, credentialposture.Descriptor{Owner: "chat:azure", Label: "Azure OpenAI API key",
		EnvKey: "azure.api.env", KeychainKey: "azure.api.keychain", LiteralKey: "azure.api.key"})

	set, err := features.Resolve(r.Snapshot(), nil)
	if err != nil {
		panic(err)
	}

	return frameworkSections(set)
}

// TestFrameworkSections_DerivedFromWhatTheBinaryDeclares (F15 of the v0.43.0
// manual round, architecture review A6): the section list was a hand list
// that lacked azure, which spec 0196 D6 added, and features, which the
// dynamic flags read, so config validate warned about keys the framework
// owns. The credential roots come from the credential registry and the
// flags root from the flags package, beside the fixed framework sections.
func TestFrameworkSections_DerivedFromWhatTheBinaryDeclares(t *testing.T) {
	t.Parallel()

	sections := testFrameworkSections()
	for _, want := range []string{"log", "update", "server", "telemetry", "ai", "chat", "output", "debug", "ci", "features", "azure"} {
		assert.Truef(t, sections[want], "%s is a framework section", want)
	}

	assert.False(t, sections["weirdsection"])
}
