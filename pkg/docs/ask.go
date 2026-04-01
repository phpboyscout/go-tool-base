package docs

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/phpboyscout/go-tool-base/pkg/chat"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/output"
	"github.com/phpboyscout/go-tool-base/pkg/props"
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

// AskAI queries the AI about the embedded documentation. If the provider supports
// streaming, deltas are delivered via deltaFn as they arrive. logFn receives status
// messages. Either callback may be nil.
func AskAI(ctx context.Context, p *props.Props, fsys fs.FS, question string, logFn func(string, logger.Level), deltaFn func(string), providerOverride ...string) (string, error) {
	logFn("Collating documentation...", logger.InfoLevel)

	content, err := GetAllMarkdownContent(fsys)
	if err != nil {
		return "", errors.Newf("failed to load content: %w", err)
	}

	logFn("Preparing prompt...", logger.DebugLevel)

	sysPrompt := fmt.Sprintf("You are a helpful assistant for 'GTB' (also known as 'als'). "+
		"Your goal is to provide high-quality, professional, and well-structured answers to the user's questions based on the provided documentation. "+
		"\n\nFOLLOW THESE GUIDELINES:\n"+
		"1. Use clear, hierarchical **Markdown** (headings, bolding, lists).\n"+
		"2. Provide a structured overview if the answer is complex.\n"+
		"3. Use consistent terminology from the provided documentation.\n"+
		"4. Be comprehensive but concise.\n"+
		"5. Answer accurately based ONLY on the documentation below. If the answer is not in the documentation, state that clearly.\n\n"+
		"--- Documentation ---\n%s", content)

	provider := ResolveProvider(p, providerOverride...)

	cfg := chat.Config{
		Provider:     provider,
		SystemPrompt: sysPrompt,
	}

	logFn("Starting Chat...", logger.DebugLevel)

	client, err := chat.New(ctx, p, cfg)
	if err != nil {
		return "", err
	}

	logFn(fmt.Sprintf("Asking AI: %s", question), logger.DebugLevel)

	if streamer, ok := client.(chat.StreamingChatClient); ok {
		return streamer.StreamChat(ctx, question, func(e chat.StreamEvent) error {
			if e.Type == chat.EventTextDelta && deltaFn != nil {
				deltaFn(e.Delta)
			}

			return nil
		})
	}

	return output.SpinWithResult(ctx, "Waiting for AI response", func(ctx context.Context) (string, error) {
		return client.Chat(ctx, question)
	})
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
