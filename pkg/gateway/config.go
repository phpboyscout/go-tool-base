package gateway

import (
	gtbgrpc "gitlab.com/phpboyscout/go-tool-base/pkg/grpc"
	gtbhttp "gitlab.com/phpboyscout/go-tool-base/pkg/http"
	gtbtls "gitlab.com/phpboyscout/go-tool-base/pkg/tls"
)

// Settings contains the typed transport settings needed to construct a gateway
// without binding the core gateway path to any particular config system.
//
// A gateway is composed from settings that live at different GTB config
// prefixes (the gateway's own HTTP listener plus the upstream gRPC server), so
// this struct is assembled by the config adapter (SettingsFromConfig), not
// decoded from a single section with mapstructure. The json/yaml tags are for
// documentation and snapshot serialisation only.
type Settings struct {
	HTTP    gtbhttp.ServerSettings `yaml:"http" json:"http"`
	HTTPTLS gtbtls.Pair            `yaml:"http_tls" json:"http_tls"`
	GRPC    gtbgrpc.ServerSettings `yaml:"grpc" json:"grpc"`
	GRPCTLS gtbtls.Pair            `yaml:"grpc_tls" json:"grpc_tls"`
}

// SettingsSource exposes the latest gateway settings snapshot to packages that
// need reload-aware access without depending on GTB config.
type SettingsSource interface {
	Current() *Settings
	Version() uint64
}
