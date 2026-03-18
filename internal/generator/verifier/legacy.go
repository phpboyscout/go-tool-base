package verifier

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/phpboyscout/gtb/internal/generator/templates"
	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/props"
)

// LegacyVerifier implements the original 5-retry loop logic.
type LegacyVerifier struct {
	props       *props.Props
	projectPath string
}

// NewLegacy checks and returns a new LegacyVerifier.
func NewLegacy(p *props.Props, projectPath string) *LegacyVerifier {
	return &LegacyVerifier{
		props:       p,
		projectPath: projectPath,
	}
}

// VerifyAndFix runs the verification loop.
func (v *LegacyVerifier) VerifyAndFix(ctx context.Context, projectRoot, cmdDir string, data *templates.CommandData, aiClient chat.ChatClient, gen GeneratorFunc) error {
	const maxRetries = 5

	var lastVerificationErrors string

	for i := 0; i <= maxRetries; i++ {
		// 1. Generate (or Regenerate)
		if err := gen(ctx, cmdDir, data); err != nil {
			return err
		}

		// 2. Verify
		verificationErrors := v.verifyGeneratedCode(ctx)
		if data.FullFileContent == "" {
			verificationErrors = append(verificationErrors, "Internal Error: AI-generated Go code is empty")
		}

		if data.TestCode == "" {
			verificationErrors = append(verificationErrors, "Internal Error: AI-generated test code is empty")
		}

		if len(verificationErrors) == 0 {
			v.props.Logger.Infof("Successfully generated command %s and passed all verifications", data.Name)

			return nil
		}

		lastVerificationErrors = strings.Join(verificationErrors, "\n\n")
		v.props.Logger.Warnf("Verification failed:\n%s", lastVerificationErrors)

		if i < maxRetries && aiClient != nil {
			v.props.Logger.Infof("Attempting to fix code with AI (Draft %d/%d)...", i+1, maxRetries)

			var resp AIResponse

			fixPrompt := fmt.Sprintf("The code you generated failed verification with the following errors:\n%s\n\nPlease fix the code.", lastVerificationErrors)

			if err := aiClient.Ask(fixPrompt, &resp); err != nil {
				v.props.Logger.Warnf("AI fix failed: %v", err)

				break
			}

			data.FullFileContent = resp.GoCode
			data.TestCode = resp.TestCode
			data.Recommendations = resp.Recommendations

			v.cleanupForRegeneration(cmdDir)
		} else {
			break
		}
	}

	if lastVerificationErrors != "" {
		v.props.Logger.Warnf("CAUTION: Command %s was generated but failed one or more verification steps (Linter, Unit Tests, or Compilation). Please review the errors above and manually correct the code.", data.Name)
	}

	return nil
}

func (v *LegacyVerifier) verifyGeneratedCode(ctx context.Context) []string {
	var verificationErrors []string

	// 0. go mod tidy
	v.props.Logger.Info("Running go mod tidy...")

	mc := exec.CommandContext(ctx, "go", "mod", "tidy")
	mc.Dir = v.projectPath
	_ = mc.Run()

	// 1. go build
	v.props.Logger.Infof("Verifying compilation with go build ./...")

	bc := exec.CommandContext(ctx, "go", "build", "./...")
	bc.Dir = v.projectPath

	if buildOutput, err := bc.CombinedOutput(); err != nil {
		verificationErrors = append(verificationErrors, fmt.Sprintf("Compilation Error:\n%s", string(buildOutput)))
	}

	// 2. go test
	v.props.Logger.Infof("Verifying tests with go test ./...")

	tc := exec.CommandContext(ctx, "go", "test", "./...")
	tc.Dir = v.projectPath

	if testOutput, err := tc.CombinedOutput(); err != nil {
		verificationErrors = append(verificationErrors, fmt.Sprintf("Test Compilation/Execution Error:\n%s", string(testOutput)))
	}

	// 3. golangci-lint
	v.props.Logger.Infof("Running golangci-lint run --fix...")

	lc := exec.CommandContext(ctx, "golangci-lint", "run", "--fix")
	lc.Dir = v.projectPath

	if lintOutput, err := lc.CombinedOutput(); err != nil {
		verificationErrors = append(verificationErrors, fmt.Sprintf("Linter Error:\n%s", string(lintOutput)))
	}

	return verificationErrors
}

func (v *LegacyVerifier) cleanupForRegeneration(cmdDir string) {
	mainPath := filepath.Join(cmdDir, "main.go")
	_ = v.props.FS.Remove(mainPath)

	testPath := filepath.Join(cmdDir, "main_test.go")
	_ = v.props.FS.Remove(testPath)
}
