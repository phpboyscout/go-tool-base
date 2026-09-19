package doctor

import (
	"context"
	"fmt"
	goversion "go/version"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	forgeapi "gitlab.com/phpboyscout/go/forge"
	"gitlab.com/phpboyscout/go/httpclient"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// ownerRWX is the Unix file mode mask for owner read/write/execute permissions.
const ownerRWX os.FileMode = 0o700

func checkGoVersion(_ context.Context, _ *p.Props) CheckResult {
	return compareGoVersion(runtime.Version())
}

func compareGoVersion(version string) CheckResult {
	// Go 1.22+ is recommended for the latest language features.
	if goversion.Compare(version, "go1.22") >= 0 {
		return CheckResult{Name: "Go version", Status: CheckPass, Message: version}
	}

	return CheckResult{
		Name:    "Go version",
		Status:  CheckWarn,
		Message: version,
		Details: "Go 1.22+ recommended",
	}
}

// checkConfig distinguishes a store with no file layer (a first run, before
// init has written anything) from one that failed to build: doctor opts out of
// the root's missing-config gate so it can say which of the two it found.
func checkConfig(_ context.Context, props *p.Props) CheckResult {
	const name = "Configuration"

	if props.Config == nil {
		return CheckResult{Name: name, Status: CheckFail, Message: "no configuration loaded"}
	}

	files := p.ConfigFileSources(props.Config.Snapshot())
	if len(files) == 0 {
		details := "Running on embedded defaults."
		if props.GetFeatures().Enabled(p.InitCmd) {
			details = fmt.Sprintf("Running on embedded defaults; `%s init` creates one.", props.Tool.Name)
		}

		return CheckResult{Name: name, Status: CheckSkip, Message: "no config file yet", Details: details}
	}

	return CheckResult{Name: name, Status: CheckPass, Message: "loaded from " + strings.Join(files, ", ")}
}

// checkForgeAdapters reports whether every enabled forge feature has its
// adapter linked into the binary. Fixed at build time, so a failure names the
// blank imports to add rather than a config key to set (spec 0194 D9).
// checkReleaseSource asks whether the tool's release source type has a
// registered provider, independently of forge features (spec 0195 D10): a
// tool whose release source names github while no github adapter is linked
// cannot build its updater, and the forge-adapters check skips it because it
// asks only about enabled features.
func checkReleaseSource(ctx context.Context, props *p.Props) CheckResult {
	const name = "Release source"

	if props == nil || props.Tool.ReleaseSource.Type == "" {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no release source"}
	}

	if !props.GetFeatures().Enabled(p.UpdateCmd) {
		return CheckResult{Name: name, Status: CheckSkip, Message: "self-update disabled"}
	}

	sourceType := props.Tool.ReleaseSource.Type
	if sourceType == p.ReleaseSourceStatic {
		return checkStaticReleaseSource(ctx, props.Tool.ReleaseSource.BaseURL)
	}

	if forgeapi.Registered(sourceType) {
		return CheckResult{Name: name, Status: CheckPass, Message: sourceType + " provider linked"}
	}

	msg := fmt.Sprintf("no provider registered for release source type %q", sourceType)
	if module, ok := forge.ModuleFor(sourceType); ok {
		msg += fmt.Sprintf(" (import %s)", module)
	}

	return CheckResult{Name: name, Status: CheckFail, Message: msg}
}

// staticPointerTimeout bounds doctor's read of the channel's pointer: one
// small object on a CDN, and doctor must not hang on a black-holing network.
const staticPointerTimeout = 5 * time.Second

// checkStaticReleaseSource is the Release source check on the static channel
// (spec 0203): it reports the base URL, reads the pointer and the manifest it
// names, and says which tag is current. Nothing published yet is a warning
// with the URL a person would look at; a pointer whose manifest does not
// resolve is a failure naming both.
func checkStaticReleaseSource(ctx context.Context, baseURL string) CheckResult {
	const name = "Release source"

	ch, err := static.New(baseURL, httpclient.NewClient())
	if err != nil {
		return CheckResult{Name: name, Status: CheckFail, Message: "static channel: " + err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, staticPointerTimeout)
	defer cancel()

	latest, err := ch.Latest(ctx)

	switch {
	case err == nil:
		return CheckResult{
			Name: name, Status: CheckPass,
			Message: fmt.Sprintf("static channel at %s, latest %s", baseURL, latest.Tag),
			Details: fmt.Sprintf("%d downloads, %s", len(latest.Downloads), signedLabel(latest.Signature != "")),
		}
	case errors.Is(err, static.ErrNoReleasesPublished):
		return CheckResult{
			Name: name, Status: CheckWarn,
			Message: "static channel at " + baseURL + ": nothing published yet",
			Details: "Expected before the first release; the pointer is " + static.PointerURL(baseURL) + ".",
		}
	default:
		return CheckResult{Name: name, Status: CheckFail, Message: "static channel at " + baseURL + ": " + err.Error()}
	}
}

func signedLabel(signed bool) string {
	if signed {
		return "signed"
	}

	return "unsigned"
}

func checkForgeAdapters(_ context.Context, props *p.Props) CheckResult {
	const name = "Forge adapters"

	if props == nil {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no tool metadata"}
	}

	enabled := make([]string, 0, len(forge.Displays()))
	for _, d := range forge.Displays() {
		if props.GetFeatures().Enabled(d.ID) {
			enabled = append(enabled, d.Label)
		}
	}

	if len(enabled) == 0 {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no forge feature enabled"}
	}

	missing := forge.Unlinked(props.GetFeatures())
	if len(missing) == 0 {
		return CheckResult{Name: name, Status: CheckPass, Message: strings.Join(enabled, ", ") + " linked"}
	}

	imports := make([]string, 0, len(missing))
	for _, m := range missing {
		imports = append(imports, fmt.Sprintf("%s (import %s)", m.Label, m.Module))
	}

	return CheckResult{
		Name:    name,
		Status:  CheckFail,
		Message: "enabled but not linked: " + strings.Join(imports, "; "),
	}
}

// chatLinksWithoutAi: without ai the links are the tool's own wiring (#94),
// named and not judged on ai.provider, a key the tool does not read.
func chatLinksWithoutAi(name string, linked []string) CheckResult {
	if len(linked) == 0 {
		return CheckResult{Name: name, Status: CheckSkip, Message: "ai feature off and no provider linked"}
	}

	return CheckResult{Name: name, Status: CheckPass, Message: strings.Join(linked, ", ") + " linked for the tool's own use (ai feature off)"}
}

// checkChatProviders is the chat side of checkForgeAdapters (spec 0196 D8,
// D12): the provider ai.provider names, and every fallback member, must be
// one this binary registers, and the fix is the module to import. It replaced
// the three-key "API keys" count, which the credential resolution check had
// made redundant (#55).
func checkChatProviders(_ context.Context, props *p.Props) CheckResult {
	const name = "Chat providers"

	if props == nil {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no tool metadata"}
	}

	// The author's declared links when the tool has them (#81), else every
	// provider the linked modules register.
	linkedProviders, unregistered := chat.LinkedProviders(props.GetFeatures())

	linked := make([]string, len(linkedProviders))
	for i, r := range linkedProviders {
		linked[i] = string(r)
	}

	if len(unregistered) > 0 {
		return CheckResult{Name: name, Status: CheckFail, Message: "declared but not linked: " + strings.Join(withImportHints(unregistered), "; ")}
	}

	if !props.GetFeatures().Enabled(p.AiCmd) {
		return chatLinksWithoutAi(name, linked)
	}

	if props.Config == nil {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no configuration loaded"}
	}

	configured := chatProvidersInUse(props.Config.View())
	if len(configured) == 0 {
		return CheckResult{
			Name:    name,
			Status:  CheckWarn,
			Message: "no " + chat.ConfigKeyAIProvider + " configured; this binary links: " + strings.Join(linked, ", "),
		}
	}

	var missing []gochat.Provider

	for _, provider := range configured {
		if !gochat.ProviderRegistered(provider) {
			missing = append(missing, provider)
		}
	}

	if len(missing) > 0 {
		return CheckResult{Name: name, Status: CheckFail, Message: "configured but not linked: " + strings.Join(withImportHints(missing), "; ")}
	}

	return CheckResult{Name: name, Status: CheckPass, Message: strings.Join(linked, ", ") + " linked"}
}

// chatProvidersInUse is ai.provider followed by the fallback chain, without
// duplicates and without empties.
// withImportHints names each provider with the module whose blank import
// registers it, which is the fix for a provider the binary lacks.
func withImportHints(providers []gochat.Provider) []string {
	hints := make([]string, 0, len(providers))

	for _, provider := range providers {
		module, ok := chat.ProviderModule(provider)
		if !ok {
			module = "no known module"
		}

		hints = append(hints, fmt.Sprintf("%s (import %s)", provider, module))
	}

	return hints
}

func chatProvidersInUse(cfg config.Reader) []gochat.Provider {
	var out []gochat.Provider

	add := func(name string) {
		if name == "" || slices.Contains(out, gochat.Provider(name)) {
			return
		}

		out = append(out, gochat.Provider(name))
	}

	add(cfg.GetString(chat.ConfigKeyAIProvider))

	for _, name := range cfg.GetStringSlice(chat.ConfigKeyAIFallback + ".providers") {
		add(name)
	}

	return out
}

func checkNoLiteralCredentials(_ context.Context, props *p.Props) CheckResult {
	if props.Config == nil {
		return CheckResult{Name: "Credential storage", Status: CheckSkip, Message: "no configuration loaded"}
	}

	cfg := props.Config.View()

	var leaked []string

	// The inventory comes from what bundles declared, not from a list kept
	// here. Three such lists existed — this one, config migrate's
	// knownCredentials (whose own comment asked the reader to keep them in sync
	// by hand), and the forge profiles — and a credential stored under a key
	// nobody had remembered to add was invisible to all of them. A downstream
	// tool's own credentials were invisible by construction. Spec 0189 R4.
	for _, d := range credentialposture.Registered() {
		if d.LiteralKey == "" {
			continue
		}

		if strings.TrimSpace(cfg.GetString(d.LiteralKey)) != "" {
			leaked = append(leaked, d.LiteralKey)
		}
	}

	sort.Strings(leaked)

	if len(leaked) == 0 {
		return CheckResult{
			Name:    "Credential storage",
			Status:  CheckPass,
			Message: "no literal credentials in config",
		}
	}

	return CheckResult{
		Name:   "Credential storage",
		Status: CheckWarn,
		// The one warning that gates a run. Spec 0189 R3 exists so a pipeline
		// can stop deprecated storage becoming permanent; every other warning
		// here is advice and must not fail anybody's build.
		Gating:  true,
		Message: fmt.Sprintf("%d literal credential(s) in config", len(leaked)),
		Details: fmt.Sprintf(
			"Key(s): %s. Migrate to env-var references (e.g. anthropic.api.env: ANTHROPIC_API_KEY) — see https://gitlab.com/phpboyscout/go-tool-base/-/wikis/specs/0054-credential-storage-hardening.",
			strings.Join(leaked, ", ")),
	}
}

func checkPermissions(_ context.Context, props *p.Props) CheckResult {
	configDir := setup.GetDefaultConfigDir(props.FS, props.Tool.Name)
	if configDir == "" {
		return CheckResult{Name: "Permissions", Status: CheckWarn, Message: "unable to determine config directory"}
	}

	info, err := props.FS.Stat(configDir)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{Name: "Permissions", Status: CheckSkip, Message: fmt.Sprintf("config directory not created yet: %s", configDir)}
		}

		return CheckResult{Name: "Permissions", Status: CheckFail, Message: fmt.Sprintf("unable to stat config directory: %v", err)}
	}

	if !info.IsDir() {
		return CheckResult{Name: "Permissions", Status: CheckFail, Message: fmt.Sprintf("config path %q is not a directory", configDir)}
	}

	mode := info.Mode().Perm()
	// Check owner has read+write+execute on the directory
	if mode&ownerRWX != ownerRWX {
		return CheckResult{Name: "Permissions", Status: CheckFail, Message: fmt.Sprintf("config directory %q has insufficient permissions: %s (need rwx for owner)", configDir, mode)}
	}

	return CheckResult{Name: "Permissions", Status: CheckPass, Message: fmt.Sprintf("config dir: %s (%s)", configDir, mode)}
}
