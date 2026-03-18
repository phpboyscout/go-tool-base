package docs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/gtb/pkg/props"

	"github.com/phpboyscout/gtb/pkg/chat"
)

type AskResponse struct {
	Answer string `json:"answer" jsonschema:"description=The comprehensive answer to the user's question based on the documentation provided."`
}

// GetAllMarkdownContent walks the FS and concatenates all .md files.
func GetAllMarkdownContent(fsys fs.FS) (string, error) {
	var sb strings.Builder

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		fmt.Fprintf(&sb, "\n\n--- File: %s ---\n\n", path)
		sb.Write(content)

		return nil
	})

	return sb.String(), err
}

type logAdapter struct {
	fn func(string)
}

func (l *logAdapter) Write(p []byte) (n int, err error) {
	if l.fn != nil {
		l.fn(string(p))
	}

	return len(p), nil
}

// AskAI encapsulates the logic to query the AI about the documentation.
// logFn is optional, if provided it receives log output.
func AskAI(ctx context.Context, p *props.Props, fsys fs.FS, question string, logFn func(string, log.Level), providerOverride ...string) (string, error) {
	logFn("Collating documentation...", log.InfoLevel)

	content, err := GetAllMarkdownContent(fsys)
	if err != nil {
		return "", errors.Newf("failed to load content: %w", err)
	}

	logFn("Preparing prompt...", log.DebugLevel)

	sysPrompt := fmt.Sprintf("You are a helpful assistant for 'GTB' (also known as 'als'). "+
		"Your goal is to provide high-quality, professional, and well-structured answers to the user's questions based on the provided documentation. "+
		"\n\nFOLLOW THESE GUIDELINES:\n"+
		"1. Use clear, hierarchical **Markdown** (headings, bolding, lists).\n"+
		"2. Provide a structured overview if the answer is complex.\n"+
		"3. Use consistent terminology from the provided documentation.\n"+
		"4. Be comprehensive but concise.\n"+
		"5. Answer accurately based ONLY on the documentation below. If the answer is not in the documentation, state that clearly.\n\n"+
		"--- Documentation ---\n%s", content)

	// Resolve Provider
	provider := ResolveProvider(p, providerOverride...)

	cfg := chat.Config{
		Provider:          provider,
		SystemPrompt:      sysPrompt,
		ResponseSchema:    chat.GenerateSchema[AskResponse](),
		SchemaName:        "documentation_answer",
		SchemaDescription: "An answer to the user's question about the documentation",
	}

	// Setup Logger
	pClone := *p

	if logFn != nil {
		uiLogger := log.NewWithOptions(io.Discard, log.Options{
			ReportTimestamp: false,
			ReportCaller:    false,
			Level:           log.InfoLevel,
		})
		writer := &logAdapter{fn: func(s string) { logFn(s, log.InfoLevel) }}
		uiLogger.SetOutput(writer)
		pClone.Logger = uiLogger
	}

	logFn("Starting Chat...", log.DebugLevel)

	client, err := chat.New(ctx, &pClone, cfg)
	if err != nil {
		return "", err
	}

	logFn(fmt.Sprintf("Asking AI: %s", question), log.DebugLevel)

	var resp AskResponse
	if err := client.Ask(question, &resp); err != nil {
		return "", err
	}

	return resp.Answer, nil
}

// ResolveProvider determines the AI provider to use based on override, config, and defaults.
func ResolveProvider(p *props.Props, providerOverride ...string) chat.Provider {
	if len(providerOverride) > 0 && providerOverride[0] != "" {
		return chat.Provider(providerOverride[0])
	}

	if p.Config != nil {
		if pName := p.Config.GetString(chat.ConfigKeyAIProvider); pName != "" {
			return chat.Provider(pName)
		}
	}

	return chat.ProviderOpenAI
}
