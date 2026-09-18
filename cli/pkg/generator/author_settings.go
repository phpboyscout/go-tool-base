package generator

// SettingKind says whether a SkeletonConfig field is an author setting the
// manifest owns or an option of one generate run.
type SettingKind string

const (
	// KindSetting is recorded in the manifest and round-trips through
	// regenerate; it has a flag, and a wizard page when it is a decision.
	KindSetting SettingKind = "setting"
	// KindRunOption shapes one run (where to write) and is not recorded.
	KindRunOption SettingKind = "run option"
)

// AuthorSetting classifies one SkeletonConfig field: how an author reaches it
// and where the manifest keeps it. Spec 0197 D1: the manifest owns every
// author setting, and TestEverySkeletonConfigFieldIsClassified holds this
// table and SkeletonConfig to each other.
type AuthorSetting struct {
	// Field is the SkeletonConfig field, dotted for a nested one.
	Field string
	Kind  SettingKind
	// Flag is the generate project flag; empty for a run option or a field
	// the wizard alone sets.
	Flag string
	// Wizard is the page key that asks it; empty when the wizard does not.
	Wizard string
	// Manifest is the dotted path from the manifest root, and the vocabulary
	// gtb set understands.
	Manifest string
}

// authorSettings is the table. Order groups by manifest block.
var authorSettings = []AuthorSetting{
	{Field: "Name", Kind: KindSetting, Flag: "name", Wizard: "name", Manifest: "properties.name"},
	{Field: "Description", Kind: KindSetting, Flag: "description", Wizard: "description", Manifest: "properties.description"},
	{Field: "Features", Kind: KindSetting, Flag: "features", Wizard: "features", Manifest: "properties.features"},
	{Field: "EnvPrefix", Kind: KindSetting, Flag: "env-prefix", Wizard: "env-prefix", Manifest: "properties.env_prefix"},
	{Field: "ConfigLayers", Kind: KindSetting, Flag: "config-layers", Manifest: "properties.config_layers"},
	{Field: "UpdatePolicy", Kind: KindSetting, Flag: "update-policy", Wizard: "update-policy", Manifest: "properties.update_policy"},
	{Field: "MCPMode", Kind: KindSetting, Flag: "mcp-mode", Wizard: "mcp-mode", Manifest: "properties.mcp.mode"},
	{Field: "UpdateCheckInterval", Kind: KindSetting, Flag: "update-check-interval", Wizard: "update-check-interval", Manifest: "properties.update_check_interval"},
	{Field: "HelpType", Kind: KindSetting, Flag: "help-type", Wizard: "help-type", Manifest: "properties.help.type"},
	{Field: "SlackChannel", Kind: KindSetting, Flag: "slack-channel", Wizard: "slack-channel", Manifest: "properties.help.slack_channel"},
	{Field: "SlackTeam", Kind: KindSetting, Flag: "slack-team", Wizard: "slack-team", Manifest: "properties.help.slack_team"},
	{Field: "TeamsChannel", Kind: KindSetting, Flag: "teams-channel", Wizard: "teams-channel", Manifest: "properties.help.teams_channel"},
	{Field: "TeamsTeam", Kind: KindSetting, Flag: "teams-team", Wizard: "teams-team", Manifest: "properties.help.teams_team"},
	{Field: "TelemetryEndpoint", Kind: KindSetting, Flag: "telemetry-endpoint", Wizard: "telemetry-endpoint", Manifest: "properties.telemetry.endpoint"},
	{Field: "TelemetryOTelEndpoint", Kind: KindSetting, Flag: "telemetry-otel-endpoint", Wizard: "telemetry-otel-endpoint", Manifest: "properties.telemetry.otel_endpoint"},
	{Field: "Signing.Enabled", Kind: KindSetting, Flag: "signing", Wizard: "signing", Manifest: "properties.signing.enabled"},
	{Field: "Signing.ExternalKeyEmail", Kind: KindSetting, Flag: "signing-email", Wizard: "signing-email", Manifest: "properties.signing.external_key_email"},
	{Field: "Signing.RequireSignature", Kind: KindSetting, Flag: "signing-require-signature", Wizard: "signing-require-signature", Manifest: "properties.signing.require_signature"},
	{Field: "Signing.RequireChecksum", Kind: KindSetting, Flag: "signing-require-checksum", Wizard: "signing-require-checksum", Manifest: "properties.signing.require_checksum"},
	{Field: "Signing.KeySource", Kind: KindSetting, Flag: "signing-key-source", Wizard: "signing-key-source", Manifest: "properties.signing.key_source"},
	{Field: "Signing.RequireExternalCrosscheck", Kind: KindSetting, Flag: "signing-require-external-crosscheck", Manifest: "properties.signing.require_external_crosscheck"},
	{Field: "Signing.Backend", Kind: KindSetting, Flag: "signing-backend", Manifest: "properties.signing.backend"},
	{Field: "Signing.KeyID", Kind: KindSetting, Flag: "signing-key-id", Wizard: "signing-key-id", Manifest: "properties.signing.key_id"},
	{Field: "Signing.KMSRegion", Kind: KindSetting, Flag: "signing-kms-region", Manifest: "properties.signing.kms_region"},
	{Field: "Signing.PublicKey", Kind: KindSetting, Flag: "signing-public-key", Manifest: "properties.signing.public_key"},
	{Field: "Chat.Providers", Kind: KindSetting, Flag: "chat-providers", Wizard: "chat-providers", Manifest: "properties.chat.providers"},
	{Field: "Chat.Default.Provider", Kind: KindSetting, Flag: "chat-default-provider", Wizard: "chat-default", Manifest: "properties.chat.default.provider"},
	{Field: "Chat.Default.Model", Kind: KindSetting, Flag: "chat-default-model", Wizard: "chat-model", Manifest: "properties.chat.default.model"},
	{Field: "Chat.Default.BaseURL", Kind: KindSetting, Flag: "chat-base-url", Wizard: "chat-base-url", Manifest: "properties.chat.default.base_url"},
	{Field: "Chat.Default.APIVersion", Kind: KindSetting, Flag: "chat-api-version", Wizard: "chat-api-version", Manifest: "properties.chat.default.api_version"},
	{Field: "Chat.Default.Project", Kind: KindSetting, Flag: "chat-project", Wizard: "chat-project", Manifest: "properties.chat.default.project"},
	{Field: "Chat.Default.Location", Kind: KindSetting, Flag: "chat-location", Wizard: "chat-location", Manifest: "properties.chat.default.location"},
	{Field: "Bootstrap.AutoInitialise", Kind: KindSetting, Flag: "auto-initialise", Manifest: "properties.bootstrap.auto_initialise"},
	{Field: "Bootstrap.SkipConfigCheck", Kind: KindSetting, Flag: "skip-config-check", Manifest: "properties.bootstrap.skip_config_check"},
	{Field: "Bootstrap.AuxiliaryCommands", Kind: KindSetting, Flag: "auxiliary-commands", Manifest: "properties.bootstrap.auxiliary_commands"},
	{Field: "CIComponentSource", Kind: KindSetting, Flag: "ci-component-source", Manifest: "properties.ci.component_source"},
	{Field: "Templates", Kind: KindSetting, Flag: "template", Manifest: "properties.templates"},
	{Field: "ModulePath", Kind: KindSetting, Flag: "module", Wizard: "module", Manifest: "properties.module_path"},
	{Field: "ForgeCredentials", Kind: KindSetting, Flag: "forge-credentials", Wizard: "forge-credentials", Manifest: "properties.forge_credentials"},
	{Field: "ForgeBackend", Kind: KindSetting, Flag: "forge-backend", Wizard: "backend", Manifest: "release_source.backend"},
	{Field: "Repo", Kind: KindSetting, Flag: "repo", Wizard: "repo", Manifest: "release_source.repo"},
	{Field: "Host", Kind: KindSetting, Flag: "host", Wizard: "host", Manifest: "release_source.host"},
	{Field: "Private", Kind: KindSetting, Flag: "private", Manifest: "release_source.private"},
	{Field: "ReleaseChannel", Kind: KindSetting, Flag: "release-channel", Wizard: "channel", Manifest: "release_source.type"},
	{Field: "Direct.URLTemplate", Kind: KindSetting, Flag: "release-url-template", Wizard: "release-url", Manifest: "release_source.direct.url_template"},
	{Field: "Direct.ChecksumURLTemplate", Kind: KindSetting, Flag: "release-checksum-url-template", Manifest: "release_source.direct.checksum_url_template"},
	{Field: "Direct.SignatureURLTemplate", Kind: KindSetting, Flag: "release-signature-url-template", Manifest: "release_source.direct.signature_url_template"},
	{Field: "Direct.VersionURL", Kind: KindSetting, Flag: "release-version-url", Wizard: "release-version-url", Manifest: "release_source.direct.version_url"},
	{Field: "Direct.VersionFormat", Kind: KindSetting, Flag: "release-version-format", Manifest: "release_source.direct.version_format"},
	{Field: "Direct.VersionKey", Kind: KindSetting, Flag: "release-version-key", Manifest: "release_source.direct.version_key"},
	{Field: "Direct.PinnedVersion", Kind: KindSetting, Flag: "release-pinned-version", Manifest: "release_source.direct.pinned_version"},
	{Field: "GoVersion", Kind: KindSetting, Flag: "go-version", Manifest: "version.go"},
	{Field: "Path", Kind: KindRunOption, Flag: "path"},
}

// AuthorSettings returns the classification of every SkeletonConfig field.
func AuthorSettings() []AuthorSetting {
	out := make([]AuthorSetting, len(authorSettings))
	copy(out, authorSettings)

	return out
}

// skeletonConfigFromManifest is the inverse of manifestFromSkeletonConfig:
// what regenerate and gtb wizard start from. The two are kept beside each
// other by TestAuthorSettingsRoundTrip, so a field added to one is added to
// the other in the same change (spec 0197 D2).
func skeletonConfigFromManifest(m Manifest) SkeletonConfig {
	_, org, repoName := m.GetReleaseSource()

	repo := ""
	if org != "" || repoName != "" {
		repo = org + "/" + repoName
	}

	channel := ""
	if m.ReleaseSource.Type == ReleaseChannelDirect {
		channel = ReleaseChannelDirect
	} else if m.ReleaseSource.Type != "" {
		channel = ReleaseChannelForge
	}

	return SkeletonConfig{
		Name:                  m.Properties.Name,
		Repo:                  repo,
		Host:                  m.ReleaseSource.Host,
		Description:           string(m.Properties.Description),
		GoVersion:             m.Version.Go,
		Features:              m.Properties.Features,
		Private:               m.ReleaseSource.Private,
		HelpType:              m.Properties.Help.Type,
		SlackChannel:          m.Properties.Help.SlackChannel,
		SlackTeam:             m.Properties.Help.SlackTeam,
		TeamsChannel:          m.Properties.Help.TeamsChannel,
		TeamsTeam:             m.Properties.Help.TeamsTeam,
		TelemetryEndpoint:     m.Properties.Telemetry.Endpoint,
		TelemetryOTelEndpoint: m.Properties.Telemetry.OTelEndpoint,
		EnvPrefix:             m.Properties.EnvPrefix,
		ConfigLayers:          m.Properties.ConfigLayers,
		Signing:               m.Properties.Signing,
		Chat:                  m.Properties.Chat,
		Bootstrap:             m.Properties.Bootstrap,
		UpdatePolicy:          m.Properties.UpdatePolicy,
		MCPMode:               m.Properties.MCP.Mode,
		UpdateCheckInterval:   m.Properties.UpdateCheckInterval,
		CIComponentSource:     m.Properties.CI.ComponentSource,
		Templates:             m.Properties.Templates,
		ForgeBackend:          m.ReleaseSource.Backend,
		ForgeCredentials:      m.Properties.ForgeCredentials,
		ModulePath:            m.Properties.ModulePath,
		ReleaseChannel:        channel,
		Direct:                m.ReleaseSource.Direct,
	}
}
