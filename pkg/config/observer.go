package config

// Observable is the interface for config change observers. Implementations
// receive the updated config and an error channel when the config file changes.
type Observable interface {
	Run(Containable, chan error)
}

// Observer is a simple Observable that wraps a handler function.
type Observer struct {
	handler func(Containable, chan error)
}

// Run invokes the observer's handler with the updated config.
func (o Observer) Run(c Containable, errs chan error) {
	o.handler(c, errs)
}
