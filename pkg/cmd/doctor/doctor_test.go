package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configMocks "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/output"
	p "github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	ver "github.com/phpboyscout/go-tool-base/pkg/version"
)

func TestCheckGoVersion_Current(t *testing.T) {
	t.Parallel()

	result := checkGoVersion(context.Background(), nil)
	assert.Equal(t, "Go version", result.Name)
	// Current Go should pass
	assert.Equal(t, CheckPass, result.Status)
}

func TestCompareGoVersion_OldVersionWarns(t *testing.T) {
	t.Parallel()
	result := compareGoVersion("go1.9")
	assert.Equal(t, CheckWarn, result.Status)
}

func TestCheckGoVersion_ValidVersions(t *testing.T) {
	t.Parallel()
	versions := []string{"go1.22", "go1.23", "go1.24"}
	for _, v := range versions {
		result := compareGoVersion(v)
		assert.Equal(t, CheckPass, result.Status, "version %s should pass", v)
	}
}

func TestCheckGoVersion_OldVersions(t *testing.T) {
	t.Parallel()
	versions := []string{"go1.21.9", "go1.20", "go1.9"}
	for _, v := range versions {
		result := compareGoVersion(v)
		assert.Equal(t, CheckWarn, result.Status, "version %s should fail (warn)", v)
	}
}

func TestCheckConfig_Loaded(t *testing.T) {
	t.Parallel()

	mockCfg := configMocks.NewMockContainable(t)
	props := &p.Props{Config: mockCfg}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, "Configuration", result.Name)
	assert.Equal(t, CheckPass, result.Status)
	assert.Equal(t, "loaded successfully", result.Message)
}

func TestCheckConfig_Missing(t *testing.T) {
	t.Parallel()

	props := &p.Props{}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, "Configuration", result.Name)
	assert.Equal(t, CheckFail, result.Status)
	assert.Equal(t, "no configuration loaded", result.Message)
}

func TestCheckAPIKeys_None(t *testing.T) {
	t.Parallel()

	mockCfg := configMocks.NewMockContainable(t)
	mockCfg.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("")
	mockCfg.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("")
	mockCfg.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("")

	props := &p.Props{Config: mockCfg}

	result := checkAPIKeys(context.Background(), props)
	assert.Equal(t, "API keys", result.Name)
	assert.Equal(t, CheckWarn, result.Status)
	assert.Contains(t, result.Message, "no AI provider")
}

func TestCheckAPIKeys_Some(t *testing.T) {
	t.Parallel()

	mockCfg := configMocks.NewMockContainable(t)
	mockCfg.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("sk-test")
	mockCfg.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("")
	mockCfg.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("")

	props := &p.Props{Config: mockCfg}

	result := checkAPIKeys(context.Background(), props)
	assert.Equal(t, "API keys", result.Name)
	assert.Equal(t, CheckPass, result.Status)
	assert.Contains(t, result.Message, "1 provider(s) configured")
}

func TestCheckAPIKeys_NoConfig(t *testing.T) {
	t.Parallel()

	props := &p.Props{}

	result := checkAPIKeys(context.Background(), props)
	assert.Equal(t, CheckSkip, result.Status)
}

func TestRunChecks(t *testing.T) {
	t.Parallel()

	mockCfg := configMocks.NewMockContainable(t)
	mockCfg.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("").Maybe()
	mockCfg.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("").Maybe()
	mockCfg.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("").Maybe()
	mockCfg.EXPECT().GetString("github.auth.value").Return("").Maybe()
	mockCfg.EXPECT().GetString("gitlab.auth.value").Return("").Maybe()
	mockCfg.EXPECT().GetString("gitea.auth.value").Return("").Maybe()
	mockCfg.EXPECT().GetString("bitbucket.app_password").Return("").Maybe()

	props := &p.Props{
		Tool:    p.Tool{Name: "test-tool"},
		Version: ver.NewInfo("v1.0.0", "", ""),
		Config:  mockCfg,
		Logger:  logger.NewNoop(),
		FS:      afero.NewMemMapFs(),
	}

	report := RunChecks(context.Background(), props)

	assert.Equal(t, "test-tool", report.Tool)
	assert.Equal(t, "v1.0.0", report.Version)
	assert.NotEmpty(t, report.Checks)

	checkNames := make(map[string]bool)
	for _, c := range report.Checks {
		checkNames[c.Name] = true
	}
	assert.True(t, checkNames["Go version"], "expected 'Go version' check in report")
	assert.True(t, checkNames["Configuration"], "expected 'Configuration' check in report")
	assert.True(t, checkNames["Git"], "expected 'Git' check in report")
	assert.True(t, checkNames["API keys"], "expected 'API keys' check in report")
	assert.True(t, checkNames["Credential storage"], "expected 'Credential storage' check in report")
	assert.True(t, checkNames["Permissions"], "expected 'Permissions' check in report")
}

func TestDoctorReport_JSONOutput(t *testing.T) {
	t.Parallel()

	report := &DoctorReport{
		Tool:    "test-tool",
		Version: "1.0.0",
		Checks: []CheckResult{
			{Name: "Test check", Status: CheckPass, Message: "all good"},
			{Name: "Warn check", Status: CheckWarn, Message: "heads up", Details: "some detail"},
		},
	}

	var buf bytes.Buffer
	out := output.NewWriter(&buf, output.FormatJSON)

	err := out.Write(report, func(w io.Writer) {})
	require.NoError(t, err)

	var result DoctorReport
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-tool", result.Tool)
	assert.Len(t, result.Checks, 2)
	assert.Equal(t, CheckPass, result.Checks[0].Status)
	assert.Equal(t, "some detail", result.Checks[1].Details)
}

func TestDoctorReport_TextOutput(t *testing.T) {
	t.Parallel()

	report := &DoctorReport{
		Tool:    "test-tool",
		Version: "1.0.0",
		Checks: []CheckResult{
			{Name: "Config", Status: CheckPass, Message: "loaded"},
			{Name: "Git", Status: CheckWarn, Message: "not found"},
			{Name: "DB", Status: CheckFail, Message: "unreachable", Details: "connection refused"},
			{Name: "Optional", Status: CheckSkip, Message: "skipped"},
		},
	}

	var buf bytes.Buffer
	PrintReport(&buf, report)

	text := buf.String()
	assert.Contains(t, text, "test-tool 1.0.0")
	assert.Contains(t, text, "[OK] Config: loaded")
	assert.Contains(t, text, "[!!] Git: not found")
	assert.Contains(t, text, "[FAIL] DB: unreachable")
	assert.Contains(t, text, "connection refused")
	assert.Contains(t, text, "[SKIP] Optional: skipped")
}

type mockStatFs struct {
	afero.Fs
	statFunc func(name string) (os.FileInfo, error)
}

func (m *mockStatFs) Stat(name string) (os.FileInfo, error) {
	if m.statFunc != nil {
		return m.statFunc(name)
	}
	return m.Fs.Stat(name)
}

func TestCheckPermissions_EmptyDir(t *testing.T) {
	// Not parallel: modifies HOME environment variable via t.Setenv.
	// GetDefaultConfigDir calls os.UserHomeDir which reads $HOME on Linux.
	tempHome := t.TempDir() // real OS dir, owned by test runner with 0700 perms
	t.Setenv("HOME", tempHome)

	props := &p.Props{
		FS:   afero.NewOsFs(), // real FS so GetDefaultConfigDir can create the dir
		Tool: p.Tool{Name: "emptydirtest"},
	}

	result := checkPermissions(context.Background(), props)

	// GetDefaultConfigDir creates <tempHome>/.emptydirtest with correct perms.
	// An empty (just-created) config directory with the right owner permissions
	// should pass the check.
	assert.Equal(t, CheckPass, result.Status)
	assert.Contains(t, result.Message, "config dir:")
}

func TestCheckPermissions_NonExistent(t *testing.T) {
	t.Parallel()

	fs := &mockStatFs{
		Fs: afero.NewMemMapFs(),
		statFunc: func(name string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}

	props := &p.Props{
		Tool: p.Tool{Name: "non-existent-tool"},
		FS:   fs,
	}

	result := checkPermissions(context.Background(), props)
	assert.Equal(t, "Permissions", result.Name)
	assert.Equal(t, CheckWarn, result.Status)
	assert.Contains(t, result.Message, "does not exist")
}

func TestCheckPermissions_NotADirectory(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := &p.Props{
		Tool: p.Tool{Name: "test-file-tool"},
		FS:   fs,
	}

	configDir := setup.GetDefaultConfigDir(fs, "test-file-tool")
	_ = fs.Remove(configDir)
	_ = afero.WriteFile(fs, configDir, []byte("file data"), 0644)

	result := checkPermissions(context.Background(), props)
	assert.Equal(t, "Permissions", result.Name)
	assert.Equal(t, CheckFail, result.Status)
	assert.Contains(t, result.Message, "not a directory")
}

func TestCheckPermissions_ValidDir(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := &p.Props{
		Tool: p.Tool{Name: "test-tool-valid"},
		FS:   fs,
	}

	configDir := setup.GetDefaultConfigDir(fs, "test-tool-valid")
	_ = fs.MkdirAll(configDir, 0700)

	result := checkPermissions(context.Background(), props)
	assert.Equal(t, "Permissions", result.Name)
	assert.Equal(t, CheckPass, result.Status)
}

type mockFileInfo struct {
	os.FileInfo
	mode os.FileMode
}

func (m *mockFileInfo) Mode() os.FileMode {
	return m.mode
}

func (m *mockFileInfo) IsDir() bool {
	return true
}

func TestCheckPermissions_InsufficientPerms(t *testing.T) {
	t.Parallel()

	fs := &mockStatFs{
		Fs: afero.NewMemMapFs(),
		statFunc: func(name string) (os.FileInfo, error) {
			return &mockFileInfo{mode: 0400}, nil
		},
	}

	props := &p.Props{
		Tool: p.Tool{Name: "test-tool-bad-perms"},
		FS:   fs,
	}

	result := checkPermissions(context.Background(), props)
	assert.Equal(t, "Permissions", result.Name)
	assert.Equal(t, CheckFail, result.Status)
	assert.Contains(t, result.Message, "insufficient permissions")
}

func TestRunChecks_WithRegisteredChecks(t *testing.T) {
	t.Parallel()

	customFeature := p.FeatureCmd("custom-test")

	setup.RegisterChecks(customFeature, []setup.CheckProvider{
		func(_ *p.Props) []setup.CheckFunc {
			return []setup.CheckFunc{
				func(_ context.Context, _ *p.Props) setup.CheckResult {
					return setup.CheckResult{
						Name:    "Custom check",
						Status:  CheckPass,
						Message: "custom check passed",
					}
				},
			}
		},
	})

	mockCfg := configMocks.NewMockContainable(t)
	mockCfg.EXPECT().GetString(chat.ConfigKeyClaudeKey).Return("").Maybe()
	mockCfg.EXPECT().GetString(chat.ConfigKeyOpenAIKey).Return("").Maybe()
	mockCfg.EXPECT().GetString(chat.ConfigKeyGeminiKey).Return("").Maybe()
	mockCfg.EXPECT().GetString("github.auth.value").Return("").Maybe()
	mockCfg.EXPECT().GetString("gitlab.auth.value").Return("").Maybe()
	mockCfg.EXPECT().GetString("gitea.auth.value").Return("").Maybe()
	mockCfg.EXPECT().GetString("bitbucket.app_password").Return("").Maybe()

	props := &p.Props{
		Tool: p.Tool{
			Name:     "test-tool",
			Features: []p.Feature{{Cmd: customFeature, Enabled: true}},
		},
		Version: ver.NewInfo("v1.0.0", "", ""),
		Config:  mockCfg,
		Logger:  logger.NewNoop(),
		FS:      afero.NewMemMapFs(),
	}

	report := RunChecks(context.Background(), props)

	// Should contain built-in checks plus the registered custom check
	var foundCustom bool

	for _, check := range report.Checks {
		if check.Name == "Custom check" {
			foundCustom = true
			assert.Equal(t, CheckPass, check.Status)
			assert.Equal(t, "custom check passed", check.Message)
		}
	}

	assert.True(t, foundCustom, "registered custom check should appear in report")
}

func TestDiscoverChecks_DisabledFeature(t *testing.T) {
	t.Parallel()

	disabledFeature := p.FeatureCmd("disabled-test")

	setup.RegisterChecks(disabledFeature, []setup.CheckProvider{
		func(_ *p.Props) []setup.CheckFunc {
			return []setup.CheckFunc{
				func(_ context.Context, _ *p.Props) setup.CheckResult {
					return setup.CheckResult{Name: "Should not appear", Status: CheckFail}
				},
			}
		},
	})

	props := &p.Props{
		Tool: p.Tool{
			Name:     "test-tool",
			Features: []p.Feature{{Cmd: disabledFeature, Enabled: false}},
		},
	}

	checks := discoverChecks(props)

	for _, check := range checks {
		result := check(context.Background(), props)
		assert.NotEqual(t, "Should not appear", result.Name, "disabled feature checks should not be discovered")
	}
}
