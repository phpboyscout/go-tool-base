package release

// ReleaseSourceConfig carries the information a ProviderFactory needs to
// construct its client. It is populated from props.ReleaseSource, and exists
// as a separate type to avoid a circular import between pkg/vcs/release and
// pkg/props.
type ReleaseSourceConfig struct {
	Type    string
	Host    string
	Owner   string
	Repo    string
	Private bool
	// Params holds provider-specific configuration key/value pairs.
	// Keys use snake_case. Valid keys are documented per provider.
	Params map[string]string
}
