// Package config provides configuration loading, merging, and access via the
// [Containable] interface backed by Viper.
//
// Configurations can be loaded from multiple sources — local files, embedded
// assets, environment variables, and command-line flags — and merged with
// deterministic precedence. Factory functions include [NewFilesContainer],
// [LoadFilesContainer], [NewReaderContainer], and [NewContainerFromViper].
//
// Type-safe accessors (GetString, GetInt, GetBool, GetDuration, GetTime, etc.)
// are exposed through [Containable]. For advanced use cases, [Containable.GetViper]
// provides direct access to the underlying Viper instance as an intentional
// power-user escape hatch.
//
// Typed section decoding is available through [UnmarshalSection]. Long-lived
// components that need reload-aware typed settings should use [ObserveSection],
// which performs the initial decode, registers a config observer, and publishes
// validated immutable snapshots through [ObservedSection]. Observed sections
// compare whole typed snapshots, expose a monotonically increasing version, and
// deliver [SectionChange] values to apply callbacks only when the struct
// changes.
//
// Hot-reload is supported on file-backed containers via the [Observable]
// interface. The container owns an fsnotify watcher over every configured file;
// on a change it re-reads and re-merges all files into a candidate, validates
// the candidate against the schema (if any), and only on success swaps the live
// config and notifies registered observers. Unparseable or invalid reloads are
// rejected fail-closed, keeping the last-known-good values. The debounce window
// is configurable via [WithReloadDebounce]; call [Container.Close] to stop the
// watcher.
package config
