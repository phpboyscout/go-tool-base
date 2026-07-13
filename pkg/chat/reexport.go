// Package chat is go-tool-base's thin adapter over the standalone multi-provider
// chat client gitlab.com/phpboyscout/go/chat. It maps GTB's Props/Viper config
// into the module's typed Settings (see config_adapter.go), owns the GTB config
// key schema (see constants.go), and blank-imports every provider module (see
// providers.go). The declarations below re-export the module's public API so the
// existing GTB call sites keep referencing chat.Config, chat.New, etc. from this
// package unchanged.
package chat

import (
	gochat "gitlab.com/phpboyscout/go/chat"
)

// Re-exported types from gitlab.com/phpboyscout/go/chat.
type (
	// ChatClient is the multi-provider chat client interface.
	ChatClient = gochat.ChatClient
	// Config carries provider behaviour for a client.
	Config = gochat.Config
	// Settings bundles Config with the package-owned logger.
	Settings = gochat.Settings
	// Provider names an AI service provider.
	Provider = gochat.Provider
	// ProviderFactory constructs a ChatClient for a provider.
	ProviderFactory = gochat.ProviderFactory
	// Tool is a callable exposed to the model.
	Tool = gochat.Tool
	// ToolCall is a single tool invocation request.
	ToolCall = gochat.ToolCall
	// ToolResult is the result of a tool execution.
	ToolResult = gochat.ToolResult
	// Media is a multimodal attachment.
	Media = gochat.Media
	// Usage reports token consumption.
	Usage = gochat.Usage
	// UsageTracker accumulates per-round-trip usage (embed to satisfy Usage()).
	UsageTracker = gochat.UsageTracker
	// CredentialConfig is the package-owned provider credential shape.
	CredentialConfig = gochat.CredentialConfig
	// KeychainLookup resolves a keychain reference to a secret.
	KeychainLookup = gochat.KeychainLookup
	// RuntimeConfig is the package-owned shape for the ai.* config section.
	RuntimeConfig = gochat.RuntimeConfig
	// FallbackConfig is the package-owned shape for ai.fallback.*.
	FallbackConfig = gochat.FallbackConfig
	// FallbackOption configures a fallback composite.
	FallbackOption = gochat.FallbackOption
	// FailoverPolicy classifies a provider error for failover.
	FailoverPolicy = gochat.FailoverPolicy
	// FailoverDecision is the outcome of classifying a provider error.
	FailoverDecision = gochat.FailoverDecision
	// HTTPStatusExtractor pulls an HTTP status from a provider error.
	HTTPStatusExtractor = gochat.HTTPStatusExtractor
	// StreamingChatClient is the optional streaming interface.
	StreamingChatClient = gochat.StreamingChatClient
	// StreamEvent is a single streaming event.
	StreamEvent = gochat.StreamEvent
	// StreamEventType enumerates streaming event kinds.
	StreamEventType = gochat.StreamEventType
	// StreamCallback receives streaming events.
	StreamCallback = gochat.StreamCallback
	// StreamToolCall describes a tool call surfaced during streaming.
	StreamToolCall = gochat.StreamToolCall
	// PersistentChatClient is the optional Save/Restore interface.
	PersistentChatClient = gochat.PersistentChatClient
	// ConversationStore persists conversation snapshots.
	ConversationStore = gochat.ConversationStore
	// FileStoreOption configures a FileStore.
	FileStoreOption = gochat.FileStoreOption
	// Snapshot is a serialisable conversation snapshot.
	Snapshot = gochat.Snapshot
	// SnapshotSummary is the metadata for a stored snapshot.
	SnapshotSummary = gochat.SnapshotSummary
	// ResolvedMedia is an attachment that passed the safety filter.
	ResolvedMedia = gochat.ResolvedMedia
)

// Re-exported constants (provider names, streaming events, defaults).
const (
	ProviderOpenAI           = gochat.ProviderOpenAI
	ProviderOpenAICompatible = gochat.ProviderOpenAICompatible
	ProviderClaude           = gochat.ProviderClaude
	ProviderClaudeLocal      = gochat.ProviderClaudeLocal
	ProviderGemini           = gochat.ProviderGemini

	EventTextDelta     = gochat.EventTextDelta
	EventToolCallStart = gochat.EventToolCallStart
	EventToolCallEnd   = gochat.EventToolCallEnd
	EventComplete      = gochat.EventComplete
	EventError         = gochat.EventError

	FailoverNext  = gochat.FailoverNext
	FailoverFatal = gochat.FailoverFatal

	DefaultModelClaude        = gochat.DefaultModelClaude
	DefaultModelOpenAI        = gochat.DefaultModelOpenAI
	DefaultModelGemini        = gochat.DefaultModelGemini
	DefaultMaxSteps           = gochat.DefaultMaxSteps
	DefaultMaxTokensClaude    = gochat.DefaultMaxTokensClaude
	DefaultMaxTokensOpenAI    = gochat.DefaultMaxTokensOpenAI
	DefaultMaxTokensGemini    = gochat.DefaultMaxTokensGemini
	DefaultChatRequestTimeout = gochat.DefaultChatRequestTimeout
	MaxBaseURLLength          = gochat.MaxBaseURLLength
)

// Re-exported sentinels and the default failover policy.
var (
	ErrInvalidBaseURL     = gochat.ErrInvalidBaseURL
	ErrInvalidSnapshotID  = gochat.ErrInvalidSnapshotID
	ErrMediaRejected      = gochat.ErrMediaRejected
	ErrMediaUnsupported   = gochat.ErrMediaUnsupported
	DefaultFailoverPolicy = gochat.DefaultFailoverPolicy
)

// Re-exported constructors and helpers (function values forwarding to the module).
var (
	New                     = gochat.New
	NewSnapshot             = gochat.NewSnapshot
	NewFileStore            = gochat.NewFileStore
	WithEncryption          = gochat.WithEncryption
	WithLogger              = gochat.WithLogger
	GenerateEncryptionKey   = gochat.GenerateEncryptionKey
	ValidateBaseURL         = gochat.ValidateBaseURL
	ValidateSnapshotID      = gochat.ValidateSnapshotID
	ValidateMediaSet        = gochat.ValidateMediaSet
	NewFallback             = gochat.NewFallback
	NewFallbackFromSettings = gochat.NewFallbackFromSettings
	NewFallbackFromConfigs  = gochat.NewFallbackFromConfigs
	NewWithFallbackSettings = gochat.NewWithFallbackSettings
	WithFallbackLogger      = gochat.WithFallbackLogger
	WithFailoverPolicy      = gochat.WithFailoverPolicy
	WithOnFailover          = gochat.WithOnFailover
	WithStrictToolContext   = gochat.WithStrictToolContext
	RegisterProvider        = gochat.RegisterProvider
	RegisterStatusExtractor = gochat.RegisterStatusExtractor
	ResolveAPIKey           = gochat.ResolveAPIKey
	ChatHTTPClient          = gochat.ChatHTTPClient
	DispatchToolExecution   = gochat.DispatchToolExecution
	ExecuteTool             = gochat.ExecuteTool
	ExecuteToolsParallel    = gochat.ExecuteToolsParallel
	NewUsage                = gochat.NewUsage
)

// GenerateSchema forwards to the module's generic schema generator (a generic
// function cannot be re-exported as a value, so it is wrapped).
func GenerateSchema[T any]() any { return gochat.GenerateSchema[T]() }
