package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const urlDefaults = `
forge:
  url:
    api: https://defaults.example.org/api/v4
`

func urlProps(t *testing.T, opts ...config.StoreOption) *p.Props {
	t.Helper()

	opts = append([]config.StoreOption{
		config.WithReaders(config.NamedSource{Name: "embedded:assets/config.yaml", Content: []byte(urlDefaults)}),
	}, opts...)

	store, err := config.NewStore(t.Context(), opts...)
	require.NoError(t, err)

	return &p.Props{Tool: p.Tool{Name: "tooltest"}, Config: store}
}

func urlFile(t *testing.T, content string) config.StoreOption {
	t.Helper()

	return urlFileAt(t, "/etc/tooltest/config.yaml", content)
}

func urlFileAt(t *testing.T, path, content string) config.StoreOption {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o600))

	return config.WithFiles(configafero.Wrap(fs), path)
}

func urlEnv(environ ...string) config.StoreOption {
	return config.WithEnv("TOOLTEST", config.WithEnviron(func() []string { return environ }))
}

// #172: a URL set outside the tool's own defaults is listed with the layer
// that set it, as scheme and host only, and the check never gates.
func TestCheckURLValues(t *testing.T) {
	t.Parallel()

	t.Run("no store, nothing to check", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), &p.Props{})
		assert.Equal(t, "Config URLs", result.Name)
		assert.Equal(t, CheckSkip, result.Status)
	})

	t.Run("URLs from compiled-in defaults pass", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t))
		assert.Equal(t, CheckPass, result.Status)
		assert.Empty(t, result.Details)
	})

	t.Run("a file's URL is listed by key, layer, scheme and host", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"registry:\n  endpoint: https://user:hunter2@registry.example.net:8443/v2/index?key=sekrit#frag\n")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.False(t, result.Gating, "a configured endpoint must not fail a CI run")
		assert.Equal(t, "registry.endpoint is https://registry.example.net:8443 from /etc/tooltest/config.yaml", result.Details)

		for _, hidden := range []string{"user", "hunter2", "/v2/index", "sekrit", "frag", "defaults.example.org"} {
			assert.NotContains(t, result.Message+result.Details, hidden)
		}
	})

	t.Run("a file repeating the tool's own default is not listed", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"forge:\n  url:\n    api: https://defaults.example.org/api/v4\n")))
		assert.Equal(t, CheckPass, result.Status, "init writes the defaults into the user's file")
		assert.Empty(t, result.Details)
	})

	t.Run("the same host on another path is not the default, and is listed", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"forge:\n  url:\n    api: https://defaults.example.org/elsewhere\n")))
		assert.Equal(t, CheckWarn, result.Status)
	})

	t.Run("a file overriding a default URL is listed", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"forge:\n  url:\n    api: https://elsewhere.example.net/api/v4\n")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Equal(t, "forge.url.api is https://elsewhere.example.net from /etc/tooltest/config.yaml", result.Details)
	})

	t.Run("http is marked as plaintext and counted", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"registry:\n  endpoint: http://127.0.0.1:8080/v1\n  fallback: HTTPS://registry.example.net\n")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Contains(t, result.Details, "registry.endpoint is http://127.0.0.1:8080 from /etc/tooltest/config.yaml (plaintext)")
		assert.Contains(t, result.Details, "registry.fallback is https://registry.example.net from /etc/tooltest/config.yaml")
		assert.NotContains(t, result.Details, "registry.example.net from /etc/tooltest/config.yaml (plaintext)")
		assert.Contains(t, result.Message, "2 config value(s)")
		assert.Contains(t, result.Message, "1 over plaintext http")
	})

	t.Run("an environment variable's URL names the variable", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlEnv("TOOLTEST_REGISTRY_ENDPOINT=https://registry.example.net/v1")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Equal(t, "registry.endpoint is https://registry.example.net from env:TOOLTEST_REGISTRY_ENDPOINT", result.Details)
	})

	t.Run("a flag's URL names the flag", func(t *testing.T) {
		t.Parallel()

		flags := pflag.NewFlagSet("tooltest", pflag.ContinueOnError)
		flags.String("registry-endpoint", "", "")
		require.NoError(t, flags.Parse([]string{"--registry-endpoint=https://registry.example.net/v1"}))

		result := checkURLValues(context.Background(), urlProps(t, config.WithBackend(config.NewFlagBackend(flags))))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Equal(t, "registry.endpoint is https://registry.example.net from flag:--registry-endpoint", result.Details)
	})

	t.Run("every URL in a repeated flag is listed", func(t *testing.T) {
		t.Parallel()

		flags := pflag.NewFlagSet("tooltest", pflag.ContinueOnError)
		flags.StringSlice("mirror", nil, "")
		require.NoError(t, flags.Parse([]string{"--mirror=https://one.example.net/a", "--mirror=http://two.example.net/b"}))

		result := checkURLValues(context.Background(), urlProps(t, config.WithBackend(config.NewFlagBackend(flags))))
		assert.Equal(t,
			"mirror is https://one.example.net from flag:--mirror\n"+
				"mirror is http://two.example.net from flag:--mirror (plaintext)",
			result.Details)
	})

	t.Run("every URL in a list is listed", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"mirrors:\n  - https://one.example.net/a\n  - not a url\n  - http://two.example.net/b\n")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Equal(t,
			"mirrors is https://one.example.net from /etc/tooltest/config.yaml\n"+
				"mirrors is http://two.example.net from /etc/tooltest/config.yaml (plaintext)",
			result.Details)
	})

	t.Run("a URL nested inside a list entry is listed under the list's key", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"upstreams:\n  - name: one\n    endpoint:\n      base: http://one.example.net/v1\n")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Equal(t, "upstreams is http://one.example.net from /etc/tooltest/config.yaml (plaintext)", result.Details)
	})

	t.Run("values that are not network URLs are not listed", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			"service:\n  address: other:4317\n  socket: file:///tmp/tool.sock\n  note: read https://docs.example.org first\n  port: 8080\n  on: true\n")))
		assert.Equal(t, CheckPass, result.Status)
		assert.Empty(t, result.Details)
	})

	t.Run("a value that does not parse as a URL is not printed", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t, "registry:\n"+
			"  port: https://user:SECRETTOKEN@registry.example.net:notaport/v2\n"+
			"  escape: https://SECRETTOKEN%zz@registry.example.net\n"+
			"  bracket: https://[SECRETTOKEN/v2\n")))
		assert.Equal(t, CheckPass, result.Status)
		assert.Empty(t, result.Details)
		assert.NotContains(t, result.Message, "SECRETTOKEN")
	})

	// url.Parse ends the authority at the first /, ? or #, so userinfo holding
	// one of them parses as the host.
	t.Run("a credential that parses as the host is never printed", func(t *testing.T) {
		t.Parallel()

		for _, value := range []string{
			"https://SECRETTOKEN/abc@registry.example.net/repo",
			"https://SECRETTOKEN?abc@registry.example.net/repo",
			"https://SECRETTOKEN#abc@registry.example.net/repo",
			"https://user:1234/SECRETTOKEN@registry.example.net",
			"https://user@SECRETTOKEN/abc@registry.example.net",
			"http://dXNlcjpTRUNSRVRUT0tFTg/SECRETTOKEN==@registry.example.net",
		} {
			scheme, _, _ := strings.Cut(value, ":")

			result := checkURLValues(context.Background(), urlProps(t, urlEnv("TOOLTEST_REGISTRY_ENDPOINT="+value)))
			assert.Equal(t, CheckWarn, result.Status, value)
			assert.Contains(t, result.Details,
				"registry.endpoint is "+scheme+"://(host withheld) from env:TOOLTEST_REGISTRY_ENDPOINT", value)

			for _, hidden := range []string{"SECRETTOKEN", "user", "1234", "dXNlcjpTRUNSRVRUT0tFTg"} {
				assert.NotContains(t, result.Message+result.Details, hidden, value)
			}
		}
	})

	t.Run("a withheld host over http is still marked plaintext", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t,
			urlEnv("TOOLTEST_REGISTRY_ENDPOINT=http://SECRETTOKEN/abc@registry.example.net")))
		assert.Equal(t, "registry.endpoint is http://(host withheld) from env:TOOLTEST_REGISTRY_ENDPOINT (plaintext)", result.Details)
		assert.Contains(t, result.Message, "1 over plaintext http")
	})

	t.Run("an escaped @ after the host keeps the host", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t,
			urlEnv("TOOLTEST_REGISTRY_ENDPOINT=https://registry.example.net/users/%40me")))
		assert.Equal(t, "registry.endpoint is https://registry.example.net from env:TOOLTEST_REGISTRY_ENDPOINT", result.Details)
	})

	t.Run("a host that could pass for another is escaped", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t,
			urlEnv("TOOLTEST_REGISTRY_ENDPOINT=https://registry.ex\u202eten.elpma/v2", "TOOLTEST_REGISTRY_FALLBACK=https://r\u0435gistry.example.net")))
		assert.Equal(t,
			`registry.endpoint is https://registry.ex\u202eten.elpma from env:TOOLTEST_REGISTRY_ENDPOINT`+"\n"+
				`registry.fallback is https://r\u0435gistry.example.net from env:TOOLTEST_REGISTRY_FALLBACK`,
			result.Details)
	})

	t.Run("a rune beyond four hex digits is escaped with eight", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t,
			urlEnv("TOOLTEST_REGISTRY_ENDPOINT=https://registry\U0001f600.example.net/v2")))
		assert.Equal(t, `registry.endpoint is https://registry\U0001f600.example.net from env:TOOLTEST_REGISTRY_ENDPOINT`, result.Details)
	})

	t.Run("a key holding the text of an escape is not read as one", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFile(t,
			`'registry\u000a     [OK] forged': https://registry.example.net/v2`+"\n")))
		assert.Equal(t,
			`registry\\u000a     [ok] forged is https://registry.example.net from /etc/tooltest/config.yaml`,
			result.Details)
	})

	t.Run("a key or a file path holding a newline cannot forge a line", func(t *testing.T) {
		t.Parallel()

		result := checkURLValues(context.Background(), urlProps(t, urlFileAt(t, "/etc/tool\ntest/config.yaml",
			"\"registry\\n     [OK] forged\": https://registry.example.net/v2\n")))
		assert.Equal(t, CheckWarn, result.Status)
		assert.Equal(t,
			`registry\u000a     [ok] forged is https://registry.example.net from /etc/tool\u000atest/config.yaml`,
			result.Details)
	})
}
