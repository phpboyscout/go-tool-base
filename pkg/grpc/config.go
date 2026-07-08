package grpc

// ServerSettings contains the data needed to construct and start a gRPC server
// without binding the core transport path to any particular config system.
type ServerSettings struct {
	Port       int  `mapstructure:"port" yaml:"port" json:"port"`
	Reflection bool `mapstructure:"reflection" yaml:"reflection" json:"reflection"`
}

// ServerSettingsSource exposes the latest gRPC server settings snapshot to
// packages that need reload-aware access without depending on GTB config.
type ServerSettingsSource interface {
	Current() *ServerSettings
	Version() uint64
}
