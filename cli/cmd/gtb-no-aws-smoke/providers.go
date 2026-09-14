package main

// The same set as gtb minus chat-bedrock, so this fixture links no AWS SDK
// through chat either; signing.go leaves out the KMS backend for the same
// reason. Those two are the only routes AWS code takes into a build.
import (
	_ "gitlab.com/phpboyscout/go/chat-anthropic"
	_ "gitlab.com/phpboyscout/go/chat-gemini"
	_ "gitlab.com/phpboyscout/go/chat-openai"
	_ "gitlab.com/phpboyscout/go/chat-openai-azure"
	_ "gitlab.com/phpboyscout/go/forge-bitbucket"
	_ "gitlab.com/phpboyscout/go/forge-gitea"
	_ "gitlab.com/phpboyscout/go/forge-github"
	_ "gitlab.com/phpboyscout/go/forge-gitlab"
)
