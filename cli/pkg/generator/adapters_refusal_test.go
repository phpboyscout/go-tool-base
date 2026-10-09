package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// A refused write or removal of any adapter file fails the sync rather than
// leaving the project half-matched to its manifest.
func TestSyncAdapterFiles_RefusalFailsTheSync(t *testing.T) {
	t.Parallel()

	ai := ManifestFeature{Name: string(props.AiCmd), Enabled: true}
	gitlab := ManifestFeature{Name: string(forge.GitlabFeature), Enabled: true}
	withDefault := ManifestChat{Providers: []string{"openai"}, Default: ManifestChatDefault{Provider: "openai"}}

	tests := []struct {
		name     string
		features []ManifestFeature
		chat     ManifestChat
		refuse   string
	}{
		{name: "writing chat.go", features: []ManifestFeature{ai}, refuse: "/proj/cmd/tool/chat.go"},
		{name: "removing chat.go", refuse: "/proj/cmd/tool/chat.go"},
		{name: "writing forge.go", features: []ManifestFeature{gitlab}, refuse: "/proj/cmd/tool/forge.go"},
		{name: "removing forge.go", refuse: "/proj/cmd/tool/forge.go"},
		{name: "creating the chat defaults bundle", features: []ManifestFeature{ai}, chat: withDefault, refuse: "/proj/cmd/tool/chat/assets"},
		{name: "writing the chat defaults", features: []ManifestFeature{ai}, chat: withDefault, refuse: "/proj/cmd/tool/chat/assets/config.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, fs := newPureGenerator(t, &Config{Path: "/proj"})
			g.props.FS = faultFs{Fs: fs, refuse: tt.refuse}

			m := &Manifest{Properties: ManifestProperties{Name: "tool", Features: tt.features, Chat: tt.chat}}

			err := g.syncAdapterFiles(m)
			require.Error(t, err)
			assert.ErrorIs(t, err, assert.AnError)
		})
	}
}
