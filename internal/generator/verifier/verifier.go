package verifier

import (
	"context"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// GeneratorFunc is a callback that writes the command files (main.go, test.go) based on the data.
type GeneratorFunc func(ctx context.Context, cmdDir string, data *templates.CommandData) error

// Verifier defines the interface for verifying and fixing generated code.
type Verifier interface {
	VerifyAndFix(ctx context.Context, projectRoot, cmdDir string, data *templates.CommandData, aiClient gochat.ChatClient, gen GeneratorFunc) error
}
