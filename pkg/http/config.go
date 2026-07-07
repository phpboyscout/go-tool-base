package http

// ServerSettings contains the data needed to construct an HTTP server without
// binding the core constructor to any particular config system.
type ServerSettings struct {
	Port           int `mapstructure:"port" yaml:"port" json:"port"`
	MaxHeaderBytes int `mapstructure:"max_header_bytes" yaml:"max_header_bytes" json:"max_header_bytes"`
}
