package config

// Observable is the interface for config change observers. Implementations
// receive the updated config when a watched config file changes and return an
// error if they could not apply the new configuration. The returned error is
// logged by the container; it never blocks subsequent reloads.
type Observable interface {
	Run(Containable) error
}

// Observer is a simple Observable that wraps a handler function.
type Observer struct {
	handler func(Containable) error
}

// Run invokes the observer's handler with the updated config.
func (o Observer) Run(c Containable) error {
	return o.handler(c)
}
