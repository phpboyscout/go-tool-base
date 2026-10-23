// Package chat is go-tool-base's framework-integration layer over the standalone
// multi-provider chat client gitlab.com/phpboyscout/go/chat.
//
// It maps GTB's Props configuration into the module's typed Settings (see
// config_adapter.go via [SettingsFromProps], [NewFromProps] and
// [NewWithFallbackFromProps]) and owns the GTB config-key schema (see
// constants.go). It registers no provider: the binary that ships one
// blank-imports its module (see [ProviderModule]). The adapter names go/chat
// types (chat.Config, chat.New, …) directly; they are no longer re-exported
// from this package.
//
// Code that wants the pure chat client API without the GTB config container
// imports gitlab.com/phpboyscout/go/chat directly.
package chat
