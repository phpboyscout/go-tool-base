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

	"gitlab.com/phpboyscout/go/output"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"
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

	props := &p.Props{Config: testutil.FileStoreFromYAML(t, "{}\n")}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, "Configuration", result.Name)
	assert.Equal(t, CheckPass, result.Status)
	assert.Contains(t, result.Message, "loaded from ")
}

// A store built from embedded defaults alone (no file layer) is what the root
// hands doctor on a fresh HOME: that is a first run, not a fault.
func TestCheckConfig_NoFileYetIsASkip(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Config: testutil.StoreFromYAML(t, "{}\n"),
		Tool:   p.Tool{Name: "fresh-tool", Features: p.SetFeatures(p.Enable(p.InitCmd))},
	}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, "Configuration", result.Name)
	assert.Equal(t, CheckSkip, result.Status)
	assert.Equal(t, "no config file yet", result.Message)
	assert.Contains(t, result.Details, "fresh-tool init")
}

func TestCheckConfig_NoFileAndNoInitCommand(t *testing.T) {
	t.Parallel()

	props := &p.Props{
		Config: testutil.StoreFromYAML(t, "{}\n"),
		Tool:   p.Tool{Name: "fresh-tool", Features: p.SetFeatures(p.Disable(p.InitCmd))},
	}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, CheckSkip, result.Status)
	assert.Equal(t, "no config file yet", result.Message)
	assert.Contains(t, result.Details, "embedded defaults")
}

func TestCheckConfig_Missing(t *testing.T) {
	t.Parallel()

	props := &p.Props{}

	result := checkConfig(context.Background(), props)
	assert.Equal(t, "Configuration", result.Name)
	assert.Equal(t, CheckFail, result.Status)
	assert.Equal(t, "no configuration loaded", result.Message)
}

func TestRunChecks(t *testing.T) {
	t.Parallel()

	mockCfg := testutil.StoreFromYAML(t, "{}\n")

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
	assert.True(t, checkNames["Credential storage"], "expected 'Credential storage' check in report")
	assert.True(t, checkNames["Permissions"], "expected 'Permissions' check in report")

	// Git is no longer a built-in check (git-consuming features register their
	// own), and the AI-key check is gated on the AI feature — which this
	// default-features tool does not enable.
	assert.False(t, checkNames["Git"], "Git must not be a built-in default check")
	assert.False(t, checkNames["API keys"], "API-key check must be gated on the AI feature")
}

// TestDefaultChecks_FeatureAware pins the feature-awareness of the default
// check set: the AI-key check appears only when the AI feature is enabled, and
// the git check is never a built-in.
func TestDefaultChecks_FeatureAware(t *testing.T) {
	t.Parallel()

	results := func(props *p.Props) map[string]CheckResult {
		got := map[string]CheckResult{}
		for _, check := range DefaultChecks(props) {
			r := check(context.Background(), props)
			got[r.Name] = r
		}

		return got
	}

	// Default-features tool: the chat check skips, and there is never a Git check.
	base := &p.Props{
		Tool:   p.Tool{Name: "t"},
		Config: testutil.StoreFromYAML(t, "{}\n"),
		FS:     afero.NewMemMapFs(),
	}
	def := results(base)
	assert.Equal(t, CheckSkip, def["Chat providers"].Status, "AI-disabled tool has nothing to say about chat providers")
	_, hasGit := def["Git"]
	assert.False(t, hasGit, "Git must never be a built-in check")
	_, hasAPIKeys := def["API keys"]
	assert.False(t, hasAPIKeys, "the three-key count is gone (spec 0196 D8)")

	// AI-enabled tool: the chat check speaks.
	ai := &p.Props{
		Tool:   p.Tool{Name: "t", Features: []p.Feature{{ID: p.AiCmd, Enabled: true}}},
		Config: testutil.StoreFromYAML(t, "{}\n"),
		FS:     afero.NewMemMapFs(),
	}
	assert.NotEqual(t, CheckSkip, results(ai)["Chat providers"].Status, "AI-enabled tool runs the chat providers check")
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
	out := output.New(output.WithWriter(&buf), output.WithFormat(output.FormatJSON))

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

	// Pre-create the config dir with correct owner permissions. GetDefaultConfigDir
	// is now pure (it never creates the directory), so the diagnostic check only
	// passes when the dir already exists — directory creation is a write-time
	// concern, not something a read-only doctor check should trigger.
	fs := afero.NewOsFs()
	configDir := setup.GetDefaultConfigDir(fs, "emptydirtest")
	require.NoError(t, fs.MkdirAll(configDir, 0o700))

	props := &p.Props{
		FS:   fs,
		Tool: p.Tool{Name: "emptydirtest"},
	}

	result := checkPermissions(context.Background(), props)

	// An empty config directory with the right owner permissions passes.
	assert.Equal(t, CheckPass, result.Status)
	assert.Contains(t, result.Message, "config dir:")
}

func TestCheckPermissions_MissingDirWarns(t *testing.T) {
	// Not parallel: modifies HOME. With the pure GetDefaultConfigDir, merely
	// running the diagnostic must NOT create ~/.toolname; a missing dir warns.
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	fs := afero.NewOsFs()

	props := &p.Props{
		FS:   fs,
		Tool: p.Tool{Name: "missingdirtest"},
	}

	result := checkPermissions(context.Background(), props)

	assert.Equal(t, CheckSkip, result.Status)
	assert.Contains(t, result.Message, "not created yet")

	// The check must not have created the directory as a side effect.
	configDir := setup.GetDefaultConfigDir(fs, "missingdirtest")
	exists, err := afero.DirExists(fs, configDir)
	require.NoError(t, err)
	assert.False(t, exists, "the read-only check must not create the config dir")
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
	assert.Equal(t, CheckSkip, result.Status)
	assert.Contains(t, result.Message, "not created yet")
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

// customFeatureSet declares id on a registry of the test's own beside the
// built-ins, contributes the check providers under it, and resolves a Set
// with the feature in the given state (spec 0199 D5).
func customFeatureSet(t *testing.T, id p.FeatureID, enabled bool, providers ...setup.CheckProvider) features.Set {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	require.NoError(t, reg.Declare(p.FeatureDescriptor{ID: id, ConstName: "Custom", ConstPackage: "example.com/custom", Kind: "test"}))

	for _, cp := range providers {
		reg.Contribute(id, setup.SlotCheck, cp)
	}

	set, err := features.Resolve(reg.Snapshot(), []features.State{{ID: id, Enabled: enabled}})
	require.NoError(t, err)

	return set
}

func TestRunChecks_WithRegisteredChecks(t *testing.T) {
	t.Parallel()

	customFeature := p.FeatureID("custom-test")

	set := customFeatureSet(t, customFeature, true, func(_ *p.Props) []setup.CheckFunc {
		return []setup.CheckFunc{
			func(_ context.Context, _ *p.Props) setup.CheckResult {
				return setup.CheckResult{
					Name:    "Custom check",
					Status:  CheckPass,
					Message: "custom check passed",
				}
			},
		}
	})

	mockCfg := testutil.StoreFromYAML(t, "{}\n")

	props := &p.Props{
		Tool: p.Tool{
			Name:     "test-tool",
			Features: []p.Feature{{ID: customFeature, Enabled: true}},
		},
		Features: set,
		Version:  ver.NewInfo("v1.0.0", "", ""),
		Config:   mockCfg,
		Logger:   logger.NewNoop(),
		FS:       afero.NewMemMapFs(),
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

	disabledFeature := p.FeatureID("disabled-test")

	set := customFeatureSet(t, disabledFeature, false, func(_ *p.Props) []setup.CheckFunc {
		return []setup.CheckFunc{
			func(_ context.Context, _ *p.Props) setup.CheckResult {
				return setup.CheckResult{Name: "Should not appear", Status: CheckFail}
			},
		}
	})

	props := &p.Props{
		Tool: p.Tool{
			Name:     "test-tool",
			Features: []p.Feature{{ID: disabledFeature, Enabled: false}},
		},
		Features: set,
	}

	checks := discoverChecks(props)

	for _, check := range checks {
		result := check(context.Background(), props)
		assert.NotEqual(t, "Should not appear", result.Name, "disabled feature checks should not be discovered")
	}
}
