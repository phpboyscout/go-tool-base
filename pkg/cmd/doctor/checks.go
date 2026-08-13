package doctor

import (
	"context"
	"fmt"
	goversion "go/version"
	"os"
	"runtime"
	"sort"
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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

func checkAPIKeys(_ context.Context, props *p.Props) CheckResult {
	if props.Config == nil {
		return CheckResult{Name: "API keys", Status: CheckSkip, Message: "no configuration loaded"}
	}

	keys := map[string]string{
		"anthropic": chat.ConfigKeyClaudeKey,
		"openai":    chat.ConfigKeyOpenAIKey,
		"gemini":    chat.ConfigKeyGeminiKey,
	}

	cfg := props.Config.View()
	configured := 0

	for _, configKey := range keys {
		if cfg.GetString(configKey) != "" {
			configured++
		}
	}

	if configured == 0 {
		return CheckResult{Name: "API keys", Status: CheckWarn, Message: "no AI provider API keys configured"}
	}

	return CheckResult{
		Name:    "API keys",
		Status:  CheckPass,
		Message: fmt.Sprintf("%d provider(s) configured", configured),
	}
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
		Name:    "Credential storage",
		Status:  CheckWarn,
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
