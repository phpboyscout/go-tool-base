// Package tls is GTB's thin adapter over the standalone hardened-TLS module
// gitlab.com/phpboyscout/go/tls. It holds the GTB-specific config-key adapter
// [Resolve] (plus [SharedPrefix]) that materialises a [Pair] from GTB
// configuration, the piece that cannot live in the framework-free module.
// Everything else (the hardened default config, the client config and cert
// pool builders) is used straight from the module.
package tls

import gtls "gitlab.com/phpboyscout/go/tls"

// SharedPrefix is the config prefix for TLS settings shared across every
// transport. A transport-specific prefix (e.g. "server.grpc.tls") overrides
// individual fields, so one certificate can serve all transports with
// per-transport overrides where needed. It is a GTB config-key convention, so it
// lives here with the [Resolve] adapter rather than in the config-agnostic module.
const SharedPrefix = "server.tls"

// Pair is the typed enabled/cert/key triple used to configure TLS for any
// transport. Aliased from gitlab.com/phpboyscout/go/tls so [Resolve] can return
// it without callers importing both packages.
type Pair = gtls.Pair
