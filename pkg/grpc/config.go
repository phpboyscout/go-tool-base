package grpc

// ServerSettings contains the data needed to construct and start a gRPC server
// without binding the core transport path to any particular config system.
type ServerSettings struct {
	Port       int  `mapstructure:"port" yaml:"port" json:"port"`
	Reflection bool `mapstructure:"reflection" yaml:"reflection" json:"reflection"`
}
