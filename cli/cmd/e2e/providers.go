package main

// The e2e binary links what gtb links, so the CLI scenarios see the same
// provider and forge set (spec 0194 D3).
import (
	_ "gitlab.com/phpboyscout/go/chat-anthropic"    // claude, claude-local
	_ "gitlab.com/phpboyscout/go/chat-bedrock"      // bedrock
	_ "gitlab.com/phpboyscout/go/chat-gemini"       // gemini, gemini-vertex, agy-local
	_ "gitlab.com/phpboyscout/go/chat-openai"       // openai, openai-compatible, codex-local
	_ "gitlab.com/phpboyscout/go/chat-openai-azure" // azure-openai
	_ "gitlab.com/phpboyscout/go/forge-bitbucket"
	_ "gitlab.com/phpboyscout/go/forge-gitea" // gitea, codeberg
	_ "gitlab.com/phpboyscout/go/forge-github"
	_ "gitlab.com/phpboyscout/go/forge-gitlab"
)
