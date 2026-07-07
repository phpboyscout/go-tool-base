---
title: "Typed config section adapters for module extraction"
description: "Introduce a config decoupling pattern where reusable and extracted packages own typed configuration structs, while GTB's config.Container unmarshals resolved sections into those structs at framework adapter boundaries. This avoids coupling standalone modules to GTB's Viper-based config stack while preserving GTB's precedence, env binding, hot reload, and validation behaviour."
date: 2026-07-07
status: DRAFT
tags:
  - specification
  - config
  - module-extraction
  - dependency-inversion
  - adapters
  - typed-configuration
author:
  - name: Matt Cockayne
    email: matt@phpboyscout.uk
  - name: Codex
    role: AI drafting assistant
---

# Typed config section adapters for module extraction

Authors
:   Matt Cockayne, Codex *(AI drafting assistant)*

Date
:   7 July 2026

Status
:   DRAFT

Builds on
:   [`2026-07-07-slog-first-extraction-seams.md`](2026-07-07-slog-first-extraction-seams.md)

Related
:   [`2026-07-07-package-extraction-report.md`](../reports/2026-07-07-package-extraction-report.md)

---

## 1. Context

`pkg/config` is one of GTB's core framework packages. It wraps Viper with
file/env/default precedence, afero-backed loading, env-prefix binding,
multi-file merging, embedded defaults, hot reload, observers, schema validation,
pflag binding, and testable filesystem behaviour.

Many packages proposed for extraction currently depend on `pkg/config` directly
or indirectly. That makes it tempting to extract `pkg/config` first and use it
as the shared configuration abstraction for every future module.

That would solve one coupling problem by creating another. Standalone modules
should not have to import GTB's full runtime config stack just to receive a few
settings. A package that needs a timeout, endpoint, token reference, or TLS
paths should not care whether the host application uses GTB config, envconfig,
Koanf, JSON, flags, Kubernetes config maps, or hard-coded test values.

This spec defines the decoupling pattern:

- extracted packages own typed configuration structs,
- GTB's config container unmarshals resolved sections into those structs,
- GTB adapter code performs existence checks, default merging, validation, and
  credential-resolution wiring,
- extracted packages receive typed values and remain config-system agnostic.

## 2. Problem Statement

`config.Containable` is useful inside GTB but too broad as a dependency seam for
extracted modules.

Current `Containable` includes:

```go
type Containable interface {
    Get(key string) any
    GetBool(key string) bool
    GetInt(key string) int
    GetFloat(key string) float64
    GetString(key string) string
    GetTime(key string) time.Time
    GetDuration(key string) time.Duration
    GetViper() *viper.Viper
    BindPFlag(key string, flag *pflag.Flag) error
    Has(key string) bool
    IsSet(key string) bool
    Set(key string, value any)
    WriteConfigAs(dest string) error
    ConfigFiles() []string
    Sub(key string) Containable
    AddObserver(o Observable)
    AddObserverFunc(f func(Containable) error)
    OnReloadError(f func(error))
    ToJSON() string
    Dump(w io.Writer)
    Validate(schema *Schema) *ValidationResult
}
```

Most extraction candidates need only a small typed subset of this information.
If they import `pkg/config`, they also inherit Viper and GTB-specific runtime
behaviour.

## 3. Goals

- Make typed package-owned config structs the primary configuration boundary for
  reusable and extracted packages.
- Add first-class config-container APIs for unmarshalling resolved config
  sections into typed structs.
- Preserve GTB's config precedence and env-prefix semantics in adapter code.
- Make section existence explicit so adapters can distinguish absent config,
  present-but-empty config, and invalid config.
- Avoid requiring extracted modules to import `pkg/config`.
- Provide a package-by-package migration plan for extraction candidates.
- Keep `pkg/config` extractable later as its own optional CLI config module, but
  not as a prerequisite or universal abstraction for other modules.

## 4. Non-goals

- Do not extract `pkg/config` in this spec.
- Do not remove existing `config.Containable` APIs immediately.
- Do not force every package to use config structs if the package has no config.
- Do not make `pkg/config` a dependency of extracted modules.
- Do not change user-facing config keys in this spec unless a later package
  migration explicitly requires it.

## 5. Design Principles

### 5.1 Extracted modules own their config shape

The extracted module defines the struct it needs:

```go
package chat

type OpenAIConfig struct {
    BaseURL string        `mapstructure:"base_url" json:"base_url" yaml:"base_url"`
    Model   string        `mapstructure:"model" json:"model" yaml:"model"`
    Timeout time.Duration `mapstructure:"timeout" json:"timeout" yaml:"timeout"`
}

func NewOpenAIClient(cfg OpenAIConfig, opts ...Option) (*OpenAIClient, error)
```

GTB owns how that struct is populated:

```go
section, err := config.UnmarshalSection[chat.OpenAIConfig](props.Config, "openai")
if err != nil {
    return nil, err
}

client, err := chat.NewOpenAIClient(section.Value, chat.WithLogger(log))
```

This keeps the extracted module independent of GTB while still allowing GTB to
provide its rich config system to applications built on the framework.

### 5.2 Typed options beat lookup interfaces

Lookup interfaces such as `GetString(key string)` are useful transitional seams,
but they should not be the default extraction design. Typed config structs are
preferable because they:

- document the package's configuration contract in code,
- allow package-specific validation,
- avoid stringly-typed config key access inside extracted modules,
- make tests simpler,
- make default merging explicit,
- allow non-GTB consumers to populate config any way they like.

Lookup interfaces remain acceptable for provider registries or compatibility
adapters where dynamic provider-specific keys are unavoidable.

### 5.3 Existence is separate from value

Adapters often need to know whether a section exists, not just what values
unmarshal from it. This matters for:

- optional provider blocks,
- "configured" checks in setup/init flows,
- fallback-to-default behaviour,
- preserving existing config during migration,
- deciding whether a missing token is an error or merely means "try env".

The config API should expose this explicitly.

### 5.4 Config loading stays in GTB composition

Extracted modules should receive already-resolved typed values. GTB remains
responsible for:

- file paths,
- embedded defaults,
- environment variable precedence,
- pflag binding,
- config hot reload,
- config file writes,
- user-facing migration commands,
- config schema validation at application startup.

## 6. Proposed Config API Additions

### 6.1 Section result type

Add a reusable result type to `pkg/config`:

```go
type Section[T any] struct {
    Value  T
    Exists bool
}
```

`Exists` means the section or key is present in the resolved configuration view
according to the chosen existence policy. The zero value means absent.

### 6.2 Unmarshal APIs

Add APIs to `Containable` and `Container`:

```go
type Containable interface {
    // existing methods...
    Unmarshal(target any) error
    UnmarshalKey(key string, target any) error
    SectionExists(key string) bool
}
```

Add generic helpers:

```go
func UnmarshalSection[T any](cfg Containable, key string) (Section[T], error)
func MustUnmarshalSection[T any](cfg Containable, key string) Section[T]
```

`MustUnmarshalSection` should be used sparingly, mostly in tests or package
defaults where panics are acceptable. Most production code should use
`UnmarshalSection`.

### 6.3 Existence policy

`SectionExists(key)` should answer "is there meaningful configuration for this
section?" without requiring callers to inspect Viper directly.

Recommended semantics:

- `true` when `IsSet(key)` is true,
- `true` when `Sub(key)` returns a non-nil structural subtree,
- `true` when any known nested key under `key` is set by file/env/flag/default,
- `false` when no value, no subtree, and no nested key exists.

Implementation should be careful with Viper defaults. A default-only section may
need to count as existing for runtime option materialisation, but setup
"is configured" checks may need file/env/keychain presence only. If both
semantics are required, expose two methods:

```go
SectionExists(key string) bool       // resolved view, includes defaults/env
SectionInConfig(key string) bool     // persisted file/config-source view
```

Open question: whether both should ship in the first implementation.

### 6.4 Unmarshal source

`UnmarshalKey` must preserve GTB's env-aware resolution. Viper's native `Sub`
can drop AutomaticEnv behaviour; GTB's `Sub` deliberately works around this for
typed getters. The unmarshal implementation must not regress that behaviour.

Implementation options:

1. Use Viper's `UnmarshalKey` against the root resolver after ensuring env
   bindings/defaults are visible.
2. Build a map by walking the target struct fields and resolving values via
   typed getters.
3. Use `AllSettings` only as a base and overlay env/flag values for fields.

This is the highest-risk implementation detail. The tests must prove env-prefix
values override file values during section unmarshal.

### 6.5 Tags

Package config structs should use `mapstructure` tags as the primary decode
tags, with `json`/`yaml` tags where useful for docs and examples:

```go
type TLSConfig struct {
    Enabled bool   `mapstructure:"enabled" json:"enabled" yaml:"enabled"`
    Cert    string `mapstructure:"cert" json:"cert" yaml:"cert"`
    Key     string `mapstructure:"key" json:"key" yaml:"key"`
}
```

Adapters should avoid exposing Viper-specific tags in public documentation; they
are an implementation detail of GTB's adapter.

### 6.6 Defaults and validation

Each extracted module should expose either:

```go
func DefaultConfig() Config
func (c Config) Validate() error
```

or constructor behaviour that clearly documents defaults and validation errors.

GTB adapters should generally:

1. start from module defaults,
2. unmarshal the GTB section,
3. overlay explicit runtime options,
4. resolve credentials/secrets,
5. call module validation,
6. construct the module object.

## 7. Adapter Pattern

### 7.1 Package adapter shape

For each package that needs GTB configuration, create an adapter in GTB, close to
the framework integration point. Prefer unexported helpers unless downstream GTB
applications need to reuse them.

Example:

```go
func openAIConfigFromGTB(cfg config.Containable) (chat.OpenAIConfig, bool, error) {
    section, err := config.UnmarshalSection[chat.OpenAIConfig](cfg, "openai")
    if err != nil {
        return chat.OpenAIConfig{}, false, err
    }

    out := chat.DefaultOpenAIConfig()
    if section.Exists {
        out = out.Merge(section.Value)
    }

    return out, section.Exists, out.Validate()
}
```

### 7.2 Constructor shape in extracted modules

Constructors should accept typed config plus functional options for dependencies
and runtime objects:

```go
func New(cfg Config, opts ...Option) (*Client, error)

func WithLogger(log *slog.Logger) Option
func WithHTTPClient(client *http.Client) Option
func WithCredentialResolver(resolver CredentialResolver) Option
```

Configuration values are data. Dependencies are options.

### 7.3 Hot reload

Hot reload should remain a GTB concern. Extracted modules should not import
GTB's observer system.

GTB can use config observers to rebuild typed structs and call package-specific
reload hooks:

```go
cfg.AddObserverFunc(func(next config.Containable) error {
    transportCfg, _, err := httpConfigFromGTB(next)
    if err != nil {
        return err
    }
    return server.Reconfigure(transportCfg)
})
```

Only packages that genuinely support live reconfiguration should expose reload
methods. Otherwise GTB should document that changes require restart.

## 8. Package-by-Package Boundary Plan

This section covers the packages scored above 5 in the extraction report.

### `pkg/chat`

Current config coupling:

- Reads provider selection from `ai.provider`.
- Reads fallback settings from `ai.fallback.*`.
- Reads provider-specific API keys, env refs, keychain refs, model, base URL,
  and other provider settings.
- Uses `pkg/config` directly in OpenAI credential resolution and elsewhere.

Extracted module shape:

```go
type Config struct {
    Provider Provider `mapstructure:"provider"`
    Model    string   `mapstructure:"model"`
}

type FallbackConfig struct {
    Enabled   bool       `mapstructure:"enabled"`
    Providers []Provider `mapstructure:"providers"`
}

type OpenAIConfig struct {
    APIKey  string `mapstructure:"api_key"`
    BaseURL string `mapstructure:"base_url"`
    Model   string `mapstructure:"model"`
}
```

GTB adapter responsibilities:

- Unmarshal `ai`, `ai.fallback`, `openai`, `anthropic`, and `gemini` sections.
- Preserve existing GTB config keys and credential cascade.
- Convert env/keychain/literal credential references into a `CredentialResolver`.
- Inject GTB's hardened HTTP client if desired.
- Pass `*slog.Logger` from the slog-first logger work.

Existence checks:

- `ai.provider` absence means use package or GTB default provider.
- Provider section absence means use provider defaults plus env fallback.
- Credential presence checks remain in GTB because they know GTB's credential
  storage schema.

### `pkg/controls`

Current config coupling: none.

Extracted module shape:

- No config struct required for the core controller unless restart/health policy
  defaults become configurable later.
- If needed, define `ControllerConfig` with restart policy, shutdown timeout,
  signal handling, and health intervals.

GTB adapter responsibilities:

- If GTB introduces config-driven controls defaults, unmarshal a
  `controls.Config` from a `controls` section and pass it to the controller.
- Keep service registration and transport health endpoint wiring outside the
  extracted core.

Existence checks:

- Not needed for first extraction wave.

### `pkg/redact`

Current config coupling: none.

Extracted module shape:

- Existing functions can remain config-free.
- Optional future `Config` may include custom patterns or disabled defaults.

GTB adapter responsibilities:

- If GTB allows custom redaction rules in config, unmarshal them in telemetry or
  doctor adapters and pass explicit rule sets to redact.

Existence checks:

- Not needed unless custom user rules are introduced.

### `pkg/changelog`

Current config coupling: none.

Extracted module shape:

- Existing generation options should remain explicit structs/options.
- If CLI defaults are config-driven, define `GenerateConfig` in the changelog
  module rather than taking `config.Containable`.

GTB adapter responsibilities:

- CLI command maps flags/config into `changelog.GenerateConfig`.

Existence checks:

- Not needed beyond command-level flag/config precedence.

### `pkg/authn`

Current config coupling: none.

Extracted module shape:

```go
type APIKeyConfig struct {
    Header string   `mapstructure:"header"`
    Keys   []string `mapstructure:"keys"`
}

type JWTConfig struct {
    Issuer   string   `mapstructure:"issuer"`
    Audience []string `mapstructure:"audience"`
    JWKSURL  string   `mapstructure:"jwks_url"`
}

type MTLSConfig struct {
    AllowedSubjects []string `mapstructure:"allowed_subjects"`
}
```

GTB adapter responsibilities:

- Transports unmarshal auth sections and construct middleware/verifiers.
- Credential references should be resolved by GTB before values reach authn.

Existence checks:

- Section absence means auth mode disabled unless explicitly required by server
  options.
- Present-but-invalid auth config is a startup error.

### `pkg/credentials`, `keychain`, `credtest`

Current config coupling: none in the package. GTB setup/config migration code
uses config heavily.

Extracted module shape:

```go
type Reference struct {
    Mode     Mode   `mapstructure:"mode"`
    Env      string `mapstructure:"env"`
    Keychain string `mapstructure:"keychain"`
    Value    string `mapstructure:"value"`
}
```

GTB adapter responsibilities:

- Continue to own migration from literal config to env/keychain references.
- Unmarshal credential references from provider sections and pass
  `credentials.Reference` or a `CredentialResolver` to extracted modules.

Existence checks:

- Setup `IsConfigured` checks need source-aware existence, not just resolved
  defaults. This is a prime use case for `SectionInConfig` or explicit
  `Has`/`IsSet` checks in GTB adapters.

### `pkg/regexutil`

Current config coupling: none.

Extracted module shape:

- Keep config-free.
- Optional future config may include max pattern length or compile timeout.

GTB adapter responsibilities:

- If configurable limits are introduced, unmarshal them where user patterns are
  accepted and pass explicit options to regexutil.

Existence checks:

- Not needed.

### `pkg/tls`

Current config coupling:

- Imports `pkg/config` to resolve TLS settings.

Extracted module shape:

```go
type Config struct {
    Enabled  bool   `mapstructure:"enabled"`
    CertFile string `mapstructure:"cert"`
    KeyFile  string `mapstructure:"key"`
    CAFile   string `mapstructure:"ca"`
}

func NewServerConfig(cfg Config) (*tls.Config, error)
func NewClientConfig(cfg Config) (*tls.Config, error)
```

GTB adapter responsibilities:

- Unmarshal `server.tls`, `server.http.tls`, `server.grpc.tls`, or custom
  prefix sections into `tls.Config`.
- Preserve fallback behaviour between shared and transport-specific TLS config.
- Resolve paths relative to GTB config/project conventions if needed.

Existence checks:

- Section absence means TLS disabled unless `Enabled` is true through another
  source.
- Present `enabled: true` without required cert/key is a validation error.

### `pkg/vcs/release`, `releasetest`

Current config coupling:

- Provider registry factory accepts `config.Containable`.
- Providers read host overrides, token refs, direct provider templates, filename
  patterns, and provider-specific parameters.

Extracted module shape:

```go
type SourceConfig struct {
    Type   string            `mapstructure:"type"`
    Owner  string            `mapstructure:"owner"`
    Repo   string            `mapstructure:"repo"`
    Host   string            `mapstructure:"host"`
    Params map[string]string `mapstructure:"params"`
}

type ProviderConfig struct {
    Host  string            `mapstructure:"host"`
    Token credentials.Reference `mapstructure:"token"`
    Params map[string]string `mapstructure:"params"`
}

type ProviderFactory func(SourceConfig, ProviderConfig, ...Option) (Provider, error)
```

GTB adapter responsibilities:

- Convert `props.Tool.ReleaseSource` plus runtime config overrides into
  `release.SourceConfig`.
- Unmarshal provider-specific sections (`github`, `gitlab`, `bitbucket`,
  `gitea`, `direct`) into provider config structs.
- Resolve credential refs through GTB's credential backend before constructing
  providers, or pass a credential resolver option.

Existence checks:

- Runtime `vcs.provider` override remains a GTB adapter concern.
- Provider section absence should not prevent public unauthenticated release
  lookups when a provider supports them.

### `pkg/vcs/repo` and `pkg/vcs/repo/aferobilly`

Current config coupling:

- `repo` imports `props` and reads `props.Config` for provider override, SSH key
  settings, and auth token resolution.
- `aferobilly` has no config coupling.

Extracted module shape:

```go
type AuthConfig struct {
    Provider string                `mapstructure:"provider"`
    Token    credentials.Reference `mapstructure:"token"`
    SSH      SSHConfig             `mapstructure:"ssh"`
}

type SSHConfig struct {
    Type string `mapstructure:"type"`
    Path string `mapstructure:"path"`
    Env  string `mapstructure:"env"`
}
```

GTB adapter responsibilities:

- Map `github.ssh`, `gitlab.ssh`, etc. into `repo.AuthConfig`.
- Resolve env/keychain values.
- Pass explicit clone/auth options to repo constructors.

Existence checks:

- SSH section absence means use HTTPS/token or provider defaults.
- `type: agent` should not require a path.

### `pkg/browser`

Current config coupling: none.

Extracted module shape:

```go
type Config struct {
    AllowedSchemes []string `mapstructure:"allowed_schemes"`
    MaxURLLength   int      `mapstructure:"max_url_length"`
}
```

GTB adapter responsibilities:

- Only needed if GTB allows tool authors to override browser policy.
- Defaults should remain secure without config.

Existence checks:

- Not needed for current behaviour.

### `pkg/forms`

Current config coupling: none.

Extracted module shape:

- Keep config-free.
- If defaults are needed later, use explicit `ThemeConfig` or `InteractionConfig`
  structs.

GTB adapter responsibilities:

- Setup commands choose interactive/non-interactive behaviour from GTB command
  flags/config and pass explicit options to forms.

Existence checks:

- Not needed in forms itself.

### `pkg/logger`

Current config coupling: none.

Extracted module shape:

```go
type Config struct {
    Level     string `mapstructure:"level"`
    Format    string `mapstructure:"format"`
    Timestamp bool   `mapstructure:"timestamp"`
    Caller    bool   `mapstructure:"caller"`
}
```

GTB adapter responsibilities:

- Root command unmarshals `log` into logger config.
- Construct Charm `slog.Handler` and shared `slog.LevelVar`.
- Preserve `--debug` precedence over config.

Existence checks:

- Absence means default human-friendly logger.
- Invalid level/format is a startup config error or warning according to current
  behaviour.

### `pkg/output`

Current config coupling: none.

Extracted module shape:

```go
type Config struct {
    Format  string `mapstructure:"format"`
    NoStyle bool   `mapstructure:"no_style"`
}
```

GTB adapter responsibilities:

- Commands map `--output`, `--no-style`, and config defaults into output
  renderer options.
- Cobra writers remain the output destination.

Existence checks:

- Output flags usually override config; section absence means command defaults.

### `pkg/workspace`

Current config coupling: none.

Extracted module shape:

```go
type Config struct {
    Markers []string `mapstructure:"markers"`
}
```

GTB adapter responsibilities:

- Generator/internal commands may pass marker overrides explicitly if GTB ever
  makes them configurable.

Existence checks:

- Not needed for current behaviour.

### `pkg/http`

Current config coupling:

- Reads server port, TLS, timeout/header limits, auth, rate limit, circuit
  breaker, logging, and middleware options from `config.Containable`.

Extracted module shape:

```go
type ServerConfig struct {
    Addr           string        `mapstructure:"addr"`
    Port           int           `mapstructure:"port"`
    ReadTimeout    time.Duration `mapstructure:"read_timeout"`
    WriteTimeout   time.Duration `mapstructure:"write_timeout"`
    MaxHeaderBytes int           `mapstructure:"max_header_bytes"`
    TLS            tls.Config    `mapstructure:"tls"`
    Auth           authn.Config  `mapstructure:"auth"`
    RateLimit      RateLimitConfig `mapstructure:"rate_limit"`
    CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

type ClientConfig struct {
    Timeout        time.Duration `mapstructure:"timeout"`
    Retry          RetryConfig   `mapstructure:"retry"`
    TLS            tls.Config    `mapstructure:"tls"`
    CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}
```

GTB adapter responsibilities:

- Unmarshal `server.http` into `http.ServerConfig`.
- Preserve fallback from `server.http.*` to shared `server.*` where currently
  supported.
- Resolve TLS through typed TLS config structs.
- Keep controller registration and health endpoint wiring in GTB/transport
  integration code.

Existence checks:

- Section absence means use built-in server defaults.
- Explicit `port: 0` should remain distinguishable from absent port if current
  behaviour allows ephemeral ports.

### `pkg/grpc`

Current config coupling:

- Reads port, reflection, TLS, auth, rate limit, circuit breaker, logging, and
  dial options from `config.Containable`.

Extracted module shape:

```go
type ServerConfig struct {
    Port           int                 `mapstructure:"port"`
    Reflection     bool                `mapstructure:"reflection"`
    TLS            tls.Config          `mapstructure:"tls"`
    Auth           authn.Config        `mapstructure:"auth"`
    RateLimit      RateLimitConfig     `mapstructure:"rate_limit"`
    CircuitBreaker CircuitBreakerConfig `mapstructure:"circuit_breaker"`
}

type ClientConfig struct {
    Target string     `mapstructure:"target"`
    TLS    tls.Config `mapstructure:"tls"`
}
```

GTB adapter responsibilities:

- Unmarshal `server.grpc`.
- Preserve fallback to shared server config where currently supported.
- Build `grpc.ServerOption` / dial options from typed config.

Existence checks:

- Reflection default must preserve existing generated-tool behaviour.
- TLS/auth sections should be optional unless enabled.

### `pkg/gateway`

Current config coupling:

- Reads HTTP/gRPC config through transport packages.

Extracted module shape:

```go
type Config struct {
    HTTP http.ServerConfig `mapstructure:"http"`
    GRPC grpc.ClientConfig `mapstructure:"grpc"`
}
```

GTB adapter responsibilities:

- Compose already-typed HTTP and gRPC configs.
- Register generated gateway handlers.

Existence checks:

- Gateway absence means not registered.

### `pkg/openapi`

Current config coupling: none directly; depends on HTTP.

Extracted module shape:

```go
type Config struct {
    SpecPath string `mapstructure:"spec_path"`
    Mount    string `mapstructure:"mount"`
    UI       bool   `mapstructure:"ui"`
}
```

GTB adapter responsibilities:

- Mount OpenAPI handlers on typed HTTP server config.

Existence checks:

- Absence means no OpenAPI route unless explicitly registered.

### `pkg/telemetry/otelcore`, `logs`, `metrics`, `tracing`

Current config coupling:

- `otelcore` reads `otel.*` endpoint, headers, insecure flags, and signal-specific
  overrides from `config.Containable`.

Extracted module shape:

```go
type OTelConfig struct {
    Endpoint string            `mapstructure:"endpoint"`
    Insecure bool              `mapstructure:"insecure"`
    Headers  map[string]string `mapstructure:"headers"`
    Traces   SignalConfig      `mapstructure:"traces"`
    Metrics  SignalConfig      `mapstructure:"metrics"`
    Logs     SignalConfig      `mapstructure:"logs"`
}
```

GTB adapter responsibilities:

- Unmarshal `otel` into observability config.
- Apply GTB service name/version/resource attributes from `props.Tool` and
  version metadata.

Existence checks:

- Section absence means observability disabled unless enabled by env or code.
- Signal-specific sections override root OTel config.

### `pkg/telemetry`

Current config coupling:

- Root telemetry reads product analytics enablement, local-only mode, delivery
  mode, backend config, data dir, deletion request settings, and tool metadata
  through `props` and config.

Extracted module shape:

```go
type Config struct {
    Enabled     bool   `mapstructure:"enabled"`
    LocalOnly   bool   `mapstructure:"local_only"`
    Backend     string `mapstructure:"backend"`
    Endpoint    string `mapstructure:"endpoint"`
    Delivery    string `mapstructure:"delivery"`
    DataDir     string `mapstructure:"data_dir"`
}
```

GTB adapter responsibilities:

- Map `telemetry` config, org policy, tool metadata, version, machine ID, and
  data directory into telemetry config/options.
- Keep consent prompts and config persistence in GTB command/setup layers.

Existence checks:

- Must distinguish absent consent from explicit disabled/enabled.
- This likely needs source-aware checks rather than default-only resolution.

### `pkg/telemetry/posthog`, `pkg/telemetry/datadog`

Current config coupling:

- Backend adapters do not use config directly but are constructed by root
  telemetry code using config-derived values.

Extracted module shape:

```go
type PostHogConfig struct {
    ProjectKey string `mapstructure:"project_key"`
    Endpoint   string `mapstructure:"endpoint"`
}

type DatadogConfig struct {
    APIKey   string `mapstructure:"api_key"`
    Endpoint string `mapstructure:"endpoint"`
}
```

GTB adapter responsibilities:

- Unmarshal backend-specific sections and resolve secret refs.
- Pass standard `*http.Client` or backend options.

Existence checks:

- Backend section required only when selected backend is active.

### `pkg/vcs`

Current config coupling:

- Token resolution reads env refs, literal values, and keychain refs from
  `config.Containable`.

Extracted module shape:

```go
type AuthConfig struct {
    Env      string `mapstructure:"env"`
    Value    string `mapstructure:"value"`
    Keychain string `mapstructure:"keychain"`
}
```

GTB adapter responsibilities:

- Convert provider auth sections into auth config or credential references.
- Resolve keychain values with GTB credential backend.

Existence checks:

- Env ref presence can be configured even if env var is missing; adapter decides
  whether that is a warning or error in context.

### `pkg/vcs` providers

Current config coupling:

- Providers read host/API URL overrides, token refs, filename patterns, direct
  download templates, and provider-specific params.

Extracted module shape:

```go
type GitHubConfig struct {
    APIURL    string     `mapstructure:"url.api"`
    UploadURL string     `mapstructure:"url.upload"`
    Auth      vcs.AuthConfig `mapstructure:"auth"`
}

type BitbucketConfig struct {
    Username    credentials.Reference `mapstructure:"username"`
    AppPassword credentials.Reference `mapstructure:"app_password"`
    Pattern     string                `mapstructure:"filename_pattern"`
}

type DirectConfig struct {
    VersionURL string            `mapstructure:"version_url"`
    AssetURL   string            `mapstructure:"asset_url"`
    Headers    map[string]string `mapstructure:"headers"`
}
```

GTB adapter responsibilities:

- Preserve current provider-specific config keys.
- Resolve credentials.
- Pass standard HTTP clients or extracted transport clients.

Existence checks:

- Provider configs are optional unless the selected provider requires a field.

### `pkg/errorhandling`

Current config coupling: none directly; it depends on logger and help config
from `props.Tool`.

Extracted module shape:

```go
type HelpConfig struct {
    Slack string `mapstructure:"slack"`
    Teams string `mapstructure:"teams"`
}
```

GTB adapter responsibilities:

- Continue to map `props.Tool.Help` into error handler construction.
- If help channels become runtime-configurable, unmarshal them in root/setup.

Existence checks:

- Absence means no help footer.

## 9. Config Package Work

### Phase 1: Add typed unmarshal support

- Add `Unmarshal`, `UnmarshalKey`, and `SectionExists` to `Containable`.
- Implement those methods on `Container` and sub-containers.
- Add generic `Section[T]` and `UnmarshalSection[T]`.
- Decide whether `SectionInConfig` is needed in phase 1.

### Phase 2: Prove env-aware unmarshalling

- Add tests where file config provides one value and prefixed env vars override
  nested fields during `UnmarshalKey`.
- Add tests for sub-container unmarshalling retaining env binding.
- Add tests for absent section, present empty section, present invalid section,
  and default-only section behaviour.

### Phase 3: Introduce package config structs

- Add typed config structs to first-wave packages that currently read
  `config.Containable` directly: `chat`, `tls`, `http`, `grpc`,
  `vcs/release`, `vcs` providers, `telemetry/otelcore`.
- Keep constructors additive and backwards compatible where public APIs already
  exist.

### Phase 4: Move GTB config reads into adapters

- Replace direct package-internal `cfg.GetString` / `cfg.GetBool` reads with
  adapter code that unmarshals typed structs.
- Keep existing command/setup config keys.
- Add migration notes only if any public constructor names or config semantics
  change.

### Phase 5: Extraction readiness checks

- Add import-boundary tests or CI checks for packages that should no longer
  import `pkg/config`.
- Update extraction specs to reference typed config structs rather than
  `ConfigLookup` as the primary seam.

## 10. TDD / BDD Strategy

Implementation must be test-first.

TDD requirements:

- Write config package tests for `UnmarshalKey`, `UnmarshalSection`, existence
  semantics, env-prefix precedence, sub-container behaviour, and error handling
  before implementation.
- For each migrated package, write tests for typed config defaults, validation,
  and GTB adapter mapping before replacing direct config reads.
- For compatibility wrappers, write tests that prove existing constructors still
  behave as before.
- Add import-boundary tests before removing `pkg/config` imports from extracted
  candidates.

BDD requirements:

- Config section unmarshalling itself is library behaviour and should be covered
  by unit/integration tests, not broad BDD.
- Add or update BDD scenarios when a user-visible CLI workflow changes, such as
  `init`, `config migrate-credentials`, `serve docs`, generated server startup,
  or provider setup.
- Generator-affecting config changes need generator E2E coverage: scaffold a
  project, build it, and confirm generated config-backed components still start.
- If no BDD scenario is added for a phase, implementation notes must state why
  unit/integration coverage is sufficient.

## 11. Documentation

Update:

- `docs/explanation/components/config/index.md`
- `docs/explanation/components/config/sources-and-precedence.md`
- `docs/explanation/components/config/validation.md`
- `docs/development/dependency-management.md`
- `docs/development/specs/2026-07-05-chat-module-extraction.md`
- `docs/development/specs/2026-07-07-slog-first-extraction-seams.md`
- Migration notes if public constructors or config semantics change.

Docs must explain:

- config loading remains a GTB framework concern,
- extracted modules define typed config structs,
- GTB adapters unmarshal resolved config sections into those structs,
- `Exists` controls optional sections and setup/configured checks,
- non-GTB consumers can populate structs with any config system.

## 12. Open Questions

1. Should `SectionExists` include defaults, or should the first implementation
   ship both `SectionExists` and `SectionInConfig`?
2. Should `UnmarshalKey` be added directly to `Containable`, or should it start
   as helper functions that accept `Containable` to avoid widening the public
   interface immediately?
3. Should typed config structs use `mapstructure` tags only, or standardise on
   `mapstructure`, `json`, and `yaml` tags for documentation and examples?
4. Should GTB adapters live inside each package, a new internal adapter package,
   or the framework composition layer that uses them?
5. Should `pkg/config` be extracted later as `gitlab.com/phpboyscout/config` or
   with a more explicit name such as `cli-config`?

## 13. Acceptance Criteria

- `pkg/config` can unmarshal resolved, env-aware config sections into typed
  structs.
- Adapters can distinguish absent sections from present invalid sections.
- First-wave extraction candidates have package-owned typed config structs or a
  documented reason they need no config.
- New extraction specs use typed config structs as the primary config boundary.
- Extracted modules are not required to import `pkg/config`.
- Existing GTB config keys and precedence semantics remain intact.

