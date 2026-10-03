package setup

import "gitlab.com/phpboyscout/go-tool-base/pkg/props"

// ConfigSourceSettings returns what `<tool> init config <slot>` asks for a
// shipped source kind, each written under config.sources.<slot>. It is nil
// for an override-only or unknown kind, whose own code reads its settings.
// The catalogue lives here rather than in each kind's package so the
// generator can describe a slot without linking the kind and its SDK.
func ConfigSourceSettings(kind, tool, slot string) []SourceSetting {
	settings, ok := sourceSettingsByKind[kind]
	if !ok {
		return nil
	}

	return settings(tool, slot)
}

// ConfigSourceInitialiserFor is the initialiser a shipped kind registers: it
// asks the kind's catalogue settings for the slot it is given.
func ConfigSourceInitialiserFor(kind string) ConfigSourceInitialiser {
	return func(p *props.Props, slot props.ConfigSource) Initialiser {
		tool := ""
		if p != nil {
			tool = p.Tool.Name
		}

		return SettingsInitialiser(slot, ConfigSourceSettings(kind, tool, slot.Name)...)
	}
}

func awsSourceSettings() []SourceSetting {
	return []SourceSetting{
		{Key: "region", Title: "AWS region", Description: "Leave empty to use the region the AWS chain resolves"},
		{Key: "profile", Title: "AWS profile", Description: "Leave empty to use the default credential chain"},
	}
}

func azureSourceSettings() []SourceSetting {
	return []SourceSetting{
		{Key: "tenant_id", Title: "Azure tenant", Description: "Leave empty to use the tenant the Azure chain resolves"},
	}
}

func gcpSourceSettings() []SourceSetting {
	return []SourceSetting{
		{Key: "project", Title: "GCP project", Required: true},
		{Key: "location", Title: "Location", Description: "Leave empty for the global service"},
	}
}

func fixed(settings ...SourceSetting) func(string, string) []SourceSetting {
	return func(string, string) []SourceSetting { return settings }
}

var sourceSettingsByKind = map[string]func(tool, slot string) []SourceSetting{
	"file": fixed(SourceSetting{
		Key: "path", Title: "File", Required: true,
		Description: "The config file this source reads; its extension says its format",
	}),
	"keychain": func(tool, slot string) []SourceSetting {
		return []SourceSetting{{
			Key: "service", Title: "Keychain service", Default: tool, Required: true,
			Description: "The service name the tool's entries are stored under. Which keys it holds is " +
				"config.sources." + slot + ".keys, usually shipped in the tool's defaults",
		}}
	},
	"vault": fixed(
		SourceSetting{Key: "address", Title: "Vault address", Description: "Leave empty to use VAULT_ADDR"},
		SourceSetting{Key: "mount", Title: "KV v2 mount", Default: "secret"},
		SourceSetting{Key: "path", Title: "Secret path", Description: "The secret read as configuration", Required: true},
		SourceSetting{Key: "auth.env", Title: "Token variable", Description: "A variable holding the token; leave empty to use VAULT_TOKEN"},
	),
	"consul": fixed(
		SourceSetting{Key: "address", Title: "Consul address", Description: "Leave empty to use CONSUL_HTTP_ADDR"},
		SourceSetting{Key: "prefix", Title: "KV prefix", Description: "The keys under it are read as configuration", Required: true},
		SourceSetting{Key: "auth.env", Title: "Token variable", Description: "A variable holding the ACL token; leave empty to use CONSUL_HTTP_TOKEN"},
	),
	"aws-s3": fixed(append([]SourceSetting{
		{Key: "bucket", Title: "Bucket", Required: true},
		{Key: "key", Title: "Object key", Description: "The config file's key; its extension says its format", Required: true},
	}, awsSourceSettings()...)...),
	"aws-ssm": fixed(append([]SourceSetting{
		{Key: "prefix", Title: "Parameter path prefix", Description: "Every parameter under it is read, such as /team/mytool", Required: true},
	}, awsSourceSettings()...)...),
	"aws-secrets": fixed(append([]SourceSetting{
		{Key: "name", Title: "Secret name", Description: "The secret whose JSON value is read as configuration", Required: true},
	}, awsSourceSettings()...)...),
	"azure-blob": fixed(append([]SourceSetting{
		{Key: "service_url", Title: "Storage service URL", Description: "Such as https://acme.blob.core.windows.net", Required: true},
		{Key: "container", Title: "Container", Required: true},
		{Key: "blob", Title: "Blob name", Description: "The config file's name; its extension says its format", Required: true},
	}, azureSourceSettings()...)...),
	"azure-keyvault": fixed(append([]SourceSetting{
		{Key: "vault_url", Title: "Vault URL", Description: "Such as https://acme.vault.azure.net", Required: true},
		{Key: "name", Title: "Secret name", Description: "One secret whose JSON value is read; leave empty to read every secret"},
	}, azureSourceSettings()...)...),
	"azure-appconfig": fixed(append([]SourceSetting{
		{Key: "endpoint", Title: "App Configuration endpoint", Description: "Such as https://acme.azconfig.io", Required: true},
		{Key: "prefix", Title: "Key prefix", Description: "The keys under it are read; leave empty for every key"},
		{Key: "label", Title: "Label", Description: "Leave empty for settings with no label"},
	}, azureSourceSettings()...)...),
	"gcp-gcs": fixed(
		SourceSetting{Key: "bucket", Title: "Bucket", Required: true},
		SourceSetting{Key: "object", Title: "Object name", Description: "The config file's name; its extension says its format", Required: true},
	),
	"gcp-secret": fixed(append(gcpSourceSettings(),
		SourceSetting{Key: "secret", Title: "Secret ID", Description: "One secret whose JSON payload is read; leave empty to read the project's secrets"},
	)...),
	"gcp-parameter": fixed(append(gcpSourceSettings(),
		SourceSetting{Key: "parameter", Title: "Parameter ID", Description: "The parameter whose payload is read as configuration", Required: true},
	)...),
}
