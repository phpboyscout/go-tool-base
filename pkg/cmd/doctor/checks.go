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

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go/config"
	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
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

func checkConfig(_ context.Context, props *p.Props) CheckResult {
	if props.Config == nil {
		return CheckResult{Name: "Configuration", Status: CheckFail, Message: "no configuration loaded"}
	}

	return CheckResult{Name: "Configuration", Status: CheckPass, Message: "loaded successfully"}
}

// checkForgeAdapters reports whether every enabled forge feature has its
// adapter linked into the binary. Fixed at build time, so a failure names the
// blank imports to add rather than a config key to set (spec 0194 D9).
// checkReleaseSource asks whether the tool's release source type has a
// registered provider, independently of forge features (spec 0195 D10): a
// tool whose release source names github while no github adapter is linked
// cannot build its updater, and the forge-adapters check skips it because it
// asks only about enabled features.
func checkReleaseSource(_ context.Context, props *p.Props) CheckResult {
	const name = "Release source"

	if props == nil || props.Tool.ReleaseSource.Type == "" {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no release source"}
	}

	if !props.GetFeatures().Enabled(p.UpdateCmd) {
		return CheckResult{Name: name, Status: CheckSkip, Message: "self-update disabled"}
	}

	sourceType := props.Tool.ReleaseSource.Type
	if forgeapi.Registered(sourceType) {
		return CheckResult{Name: name, Status: CheckPass, Message: sourceType + " provider linked"}
	}

	msg := fmt.Sprintf("no provider registered for release source type %q", sourceType)
	if module, ok := forge.ModuleFor(sourceType); ok {
		msg += fmt.Sprintf(" (import %s)", module)
	}

	return CheckResult{Name: name, Status: CheckFail, Message: msg}
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

// checkChatProviders is the chat side of checkForgeAdapters (spec 0196 D8,
// D12): the provider ai.provider names, and every fallback member, must be
// one this binary registers, and the fix is the module to import. It replaced
// the three-key "API keys" count, which the credential resolution check had
// made redundant (#55).
func checkChatProviders(_ context.Context, props *p.Props) CheckResult {
	const name = "Chat providers"

	if props == nil || !props.GetFeatures().Enabled(p.AiCmd) {
		return CheckResult{Name: name, Status: CheckSkip, Message: "ai feature not enabled"}
	}

	if props.Config == nil {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no configuration loaded"}
	}

	registered := gochat.RegisteredProviders()

	linked := make([]string, len(registered))
	for i, r := range registered {
		linked[i] = string(r)
	}

	configured := chatProvidersInUse(props.Config.View())
	if len(configured) == 0 {
		return CheckResult{
			Name:    name,
			Status:  CheckWarn,
			Message: "no " + chat.ConfigKeyAIProvider + " configured; this binary links: " + strings.Join(linked, ", "),
		}
	}

	var missing []string

	for _, provider := range configured {
		if gochat.ProviderRegistered(provider) {
			continue
		}

		module, ok := chat.ProviderModule(provider)
		if !ok {
			module = "no known module"
		}

		missing = append(missing, fmt.Sprintf("%s (import %s)", provider, module))
	}

	if len(missing) > 0 {
		return CheckResult{Name: name, Status: CheckFail, Message: "configured but not linked: " + strings.Join(missing, "; ")}
	}

	return CheckResult{Name: name, Status: CheckPass, Message: strings.Join(linked, ", ") + " linked"}
}

// chatProvidersInUse is ai.provider followed by the fallback chain, without
// duplicates and without empties.
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
			return CheckResult{Name: "Permissions", Status: CheckWarn, Message: fmt.Sprintf("config list: %s (does not exist)", configDir)}
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
