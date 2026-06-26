// Package ai provides the interactive AI-provider setup initialiser.
//
// It registers a setup [Initialiser] (and the `init ai` command via [NewCmdInitAI])
// that walks the user through choosing an AI provider (Claude, OpenAI, Gemini) and
// recording its credentials — via an env-var reference, the OS keychain, or a
// literal in config — using the shared credential storage modes. It configures the
// provider for later use; it does not construct chat clients (see pkg/chat).
package ai
