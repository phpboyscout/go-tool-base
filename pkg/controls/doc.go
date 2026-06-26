// Package controls provides a lifecycle controller for managing concurrent,
// long-running services such as HTTP servers, background workers, and schedulers.
//
// The [Controller] orchestrates startup, health monitoring, and graceful shutdown
// with proper cleanup ordering. Shared communication channels carry errors,
// OS signals, and control messages between registered services.
//
// The [Controllable] interface abstracts the controller for testing, exposing
// Start, Stop, IsRunning, IsStopped, IsStopping, Register, and configuration
// methods.
package controls
