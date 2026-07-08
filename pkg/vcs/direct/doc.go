// Package direct provides a release.Provider implementation for tools
// distributed via arbitrary HTTP servers. Asset URLs are constructed from typed
// [Settings] and configurable templates; version detection is optional and
// supports plain text, JSON, YAML, and XML endpoints. GTB config integration
// lives in [SettingsFromConfig].
package direct
