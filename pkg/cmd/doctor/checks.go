package doctor

import (
	"context"
	"fmt"
	goversion "go/version"
	"os"
	"os/exec"
	"runtime"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
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

func checkGit(ctx context.Context, _ *p.Props) CheckResult {
	cmd := exec.CommandContext(ctx, "git", "status")

	if err := cmd.Run(); err != nil {
		return CheckResult{
			Name:    "Git",
			Status:  CheckWarn,
			Message: "git not available or not in a repository",
		}
	}

	return CheckResult{Name: "Git", Status: CheckPass, Message: "repository accessible"}
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

	configured := 0

	for _, configKey := range keys {
		if props.Config.GetString(configKey) != "" {
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
