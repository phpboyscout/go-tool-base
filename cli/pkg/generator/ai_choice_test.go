package generator

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestAIChoice pins spec 0211 D8: the run, then the project's manifest, then
// the person's config decide whether a model is called, and ai.provider alone
// never does (#137). Not parallel: the CI rows set CI=true.
func TestAIChoice(t *testing.T) {
	yes, no := true, false

	cases := []struct {
		name       string
		cfg        Config
		client     bool
		manifestAI *bool
		configYAML string
		ci         bool
		want       bool
		rung       aiRung
	}{
		{name: "nothing asked", want: false, rung: aiRungUnasked},
		{name: "ai.provider in config is not an ask", configYAML: "ai:\n  provider: claude\n", want: false, rung: aiRungUnasked},
		{name: "--provider alone is not an ask", cfg: Config{AIProvider: "claude"}, want: false, rung: aiRungUnasked},
		{name: "--ai asks", cfg: Config{AI: true}, want: true, rung: aiRungRun},
		{name: "--prompt asks", cfg: Config{Prompt: "list files"}, want: true, rung: aiRungRun},
		{name: "--script asks", cfg: Config{ScriptPath: "backup.sh"}, want: true, rung: aiRungRun},
		{name: "--update-docs asks", cfg: Config{UpdateDocs: true}, want: true, rung: aiRungRun},
		{name: "injected client", client: true, want: true, rung: aiRungClient},
		{name: "--no-ai beats the injected client", cfg: Config{NoAI: true}, client: true, want: false, rung: aiRungRefused},
		{name: "--no-ai beats a manifest yes", cfg: Config{NoAI: true}, manifestAI: &yes, want: false, rung: aiRungRefused},
		{name: "manifest yes", manifestAI: &yes, want: true, rung: aiRungManifest},
		{name: "manifest no beats config yes", manifestAI: &no, configYAML: "generator:\n  ai: true\n", want: false, rung: aiRungManifest},
		{name: "--ai beats a manifest no", cfg: Config{AI: true}, manifestAI: &no, want: true, rung: aiRungRun},
		{name: "config yes when the manifest has not said", configYAML: "generator:\n  ai: true\n", want: true, rung: aiRungConfig},
		{name: "config yes is not read under CI", configYAML: "generator:\n  ai: true\n", ci: true, want: false, rung: aiRungUnasked},
		{name: "manifest yes counts under CI", manifestAI: &yes, ci: true, want: true, rung: aiRungManifest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.ci {
				t.Setenv("CI", "true")
			} else {
				t.Setenv("CI", "")
			}

			fs := afero.NewMemMapFs()
			cfg := tc.cfg
			cfg.Path = "/proj"

			yaml := tc.configYAML
			if yaml == "" {
				yaml = "{}\n"
			}

			g := &Generator{
				props:  &props.Props{FS: fs, Logger: logger.NewNoop(), Config: testutil.StoreFromYAML(t, yaml)},
				config: &cfg,
			}
			if tc.client {
				g.chatClient = new(MockChatClient)
			}

			if tc.manifestAI != nil {
				m := &Manifest{Properties: ManifestProperties{Name: "proj", Generator: ManifestGenerator{AI: tc.manifestAI}}}
				require.NoError(t, g.marshalManifestFile(ManifestPathFor(cfg.Path), m))
			}

			got := g.aiChoice()
			assert.Equal(t, tc.want, got.wanted)
			assert.Equal(t, tc.rung, got.rung)
		})
	}
}

// TestCheckAIFlags pins the refusals spec 0211 D4, D6 and D10 make errors:
// a run cannot both ask and refuse.
func TestCheckAIFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "nothing", cfg: Config{}},
		{name: "--ai", cfg: Config{AI: true}},
		{name: "--no-ai", cfg: Config{NoAI: true}},
		{name: "--ai with --no-ai", cfg: Config{AI: true, NoAI: true}, wantErr: true},
		{name: "--prompt with --no-ai", cfg: Config{Prompt: "x", NoAI: true}, wantErr: true},
		{name: "--script with --no-ai", cfg: Config{ScriptPath: "x.sh", NoAI: true}, wantErr: true},
		{name: "--update-docs with --no-ai", cfg: Config{UpdateDocs: true, NoAI: true}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := CheckAIFlags(&tc.cfg)
			if tc.wantErr {
				assert.ErrorIs(t, err, ErrAIFlagsConflict)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestAIChoice_ProviderWithoutAskWarns pins spec 0211 D9: --provider with no
// ask warns once and names --ai; with --prompt the run has asked, so no warning.
func TestAIChoice_ProviderWithoutAskWarns(t *testing.T) {
	t.Setenv("CI", "")

	cases := []struct {
		name     string
		cfg      Config
		wantWarn bool
	}{
		{name: "provider alone", cfg: Config{AIProvider: "claude"}, wantWarn: true},
		{name: "provider with --prompt", cfg: Config{AIProvider: "claude", Prompt: "list files"}},
		{name: "provider with --ai", cfg: Config{AIProvider: "claude", AI: true}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := logger.NewBuffer()
			cfg := tc.cfg
			cfg.Path = "/proj"
			g := &Generator{
				props:  &props.Props{FS: afero.NewMemMapFs(), Logger: log, Config: testutil.StoreFromYAML(t, "{}\n")},
				config: &cfg,
			}

			g.aiChoice()
			g.aiChoice()

			warnings := 0
			for _, msg := range log.Messages() {
				if strings.Contains(msg, "pass --ai") {
					warnings++
				}
			}

			if tc.wantWarn {
				assert.Equal(t, 1, warnings, "warns once per run")
			} else {
				assert.Zero(t, warnings)
			}
		})
	}
}
