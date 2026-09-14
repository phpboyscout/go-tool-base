package main

// gtb is the kitchen-sink binary: it links every chat provider and every forge
// adapter so the generator can offer, and `gtb ai` can reach, all of them
// (spec 0194 D3, OQ1). A generated tool links only what its manifest selects.
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
