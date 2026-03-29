package chat

import "context"

// StreamEventType identifies the kind of stream event.
type StreamEventType int

const (
	// EventTextDelta is a partial text response fragment.
	EventTextDelta StreamEventType = iota
	// EventToolCallStart indicates a tool call has begun execution.
	EventToolCallStart
	// EventToolCallEnd indicates a tool call has completed execution.
	EventToolCallEnd
	// EventComplete indicates the stream has finished successfully.
	EventComplete
	// EventError indicates an error occurred during streaming.
	EventError
)

// StreamToolCall contains information about a tool call within a stream.
type StreamToolCall struct {
	// ID is the provider-assigned identifier for the tool call.
	ID string
	// Name is the tool name.
	Name string
	// Arguments is the complete JSON argument payload (only populated on EventToolCallEnd).
	Arguments string
	// Result is the tool execution result (only populated on EventToolCallEnd).
	Result string
}

// StreamEvent represents a single event in a streaming response.
type StreamEvent struct {
	// Type indicates the kind of event.
	Type StreamEventType

	// Delta contains the text fragment for EventTextDelta events.
	Delta string

	// ToolCall contains tool call information for EventToolCallStart/EventToolCallEnd events.
	ToolCall *StreamToolCall

	// Error contains error information for EventError events.
	Error error
}

// StreamCallback receives streaming events. Return a non-nil error to cancel the stream.
type StreamCallback func(event StreamEvent) error

// StreamingChatClient extends ChatClient with streaming support.
// Implementations that support streaming implement this interface
// in addition to ChatClient. Discover support via type assertion:
//
//	if streamer, ok := client.(chat.StreamingChatClient); ok {
//	    result, err := streamer.StreamChat(ctx, "prompt", callback)
//	}
type StreamingChatClient interface {
	ChatClient

	// StreamChat sends a message and streams the response via callback.
	// The callback is invoked for each event in the stream. If the callback
	// returns a non-nil error, the stream is cancelled and that error is returned.
	// The return value is the complete assembled response text (concatenation of
	// all EventTextDelta fragments) or an error if streaming failed.
	// Tool calls are handled internally via the same ReAct loop as Chat(). If
	// Config.ParallelTools is enabled, multiple tool calls are executed concurrently.
	StreamChat(ctx context.Context, prompt string, callback StreamCallback) (string, error)
}
