package awssource

import "gitlab.com/phpboyscout/go-tool-base/pkg/setup"

// Settings are the initialiser questions every AWS kind shares, after its
// own.
func Settings() []setup.SourceSetting {
	return []setup.SourceSetting{
		{Key: "region", Title: "AWS region", Description: "Leave empty to use the region the AWS chain resolves"},
		{Key: "profile", Title: "AWS profile", Description: "Leave empty to use the default credential chain"},
	}
}
