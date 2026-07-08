package gateway

import (
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// Settings contains the typed transport settings needed to construct a gateway
// without binding the core gateway path to any particular config system.
type Settings struct {
	HTTP    gtbhttp.ServerSettings `mapstructure:"http" yaml:"http" json:"http"`
	HTTPTLS gtbtls.Pair            `mapstructure:"http_tls" yaml:"http_tls" json:"http_tls"`
	GRPC    gtbgrpc.ServerSettings `mapstructure:"grpc" yaml:"grpc" json:"grpc"`
	GRPCTLS gtbtls.Pair            `mapstructure:"grpc_tls" yaml:"grpc_tls" json:"grpc_tls"`
}

// SettingsSource exposes the latest gateway settings snapshot to packages that
// need reload-aware access without depending on GTB config.
type SettingsSource interface {
	Current() *Settings
	Version() uint64
}
