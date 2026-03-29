package release

// SourceType constants identify the built-in release provider types.
// Any string is accepted by the registry; downstream consumers may define
// their own constants for custom providers.
const (
	SourceTypeGitHub    = "github"
	SourceTypeGitLab    = "gitlab"
	SourceTypeBitbucket = "bitbucket"
	SourceTypeGitea     = "gitea"
	SourceTypeCodeberg  = "codeberg"
	SourceTypeDirect    = "direct"
)
