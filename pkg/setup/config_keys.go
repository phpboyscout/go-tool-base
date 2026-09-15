package setup

// Config keys for the self-update and telemetry sections. Declared once here
// so the root pre-run, the wizards and the doctor checks cannot drift apart
// on the spelling of a key that is part of a tool's config contract.
const (
	// ConfigKeyUpdatePolicy selects the self-update posture (props.UpdatePolicy).
	ConfigKeyUpdatePolicy = "update.policy"
	// ConfigKeyUpdateCheckInterval throttles the update check, as a Go duration.
	ConfigKeyUpdateCheckInterval = "update.check_interval"
	// ConfigKeyUpdateRequireSignature refuses an unsigned update asset.
	ConfigKeyUpdateRequireSignature = "update.require_signature"
	// ConfigKeyUpdateRequireChecksum refuses an asset with no checksum entry.
	ConfigKeyUpdateRequireChecksum = "update.require_checksum"
	// ConfigKeyUpdateRequireExternalCrosscheck fails closed when the WKD resolver is unreachable.
	ConfigKeyUpdateRequireExternalCrosscheck = "update.require_external_crosscheck"
	// ConfigKeyUpdateKeySource selects embedded, external or both trust anchors.
	ConfigKeyUpdateKeySource = "update.key_source"
	// ConfigKeyUpdateExternalKeyEmail names the WKD identity of the release key.
	ConfigKeyUpdateExternalKeyEmail = "update.external_key_email"
	// ConfigKeyUpdateChecksumAssetName overrides the checksums asset file name.
	ConfigKeyUpdateChecksumAssetName = "update.checksum_asset_name"
	// ConfigKeyUpdateSignatureAssetName overrides the signature asset file name.
	ConfigKeyUpdateSignatureAssetName = "update.signature_asset_name"

	// ConfigKeyTelemetryEnabled is the opt-in switch for usage telemetry.
	ConfigKeyTelemetryEnabled = "telemetry.enabled"
	// ConfigKeyTelemetryLocalOnly keeps events on disk and never exports them.
	ConfigKeyTelemetryLocalOnly = "telemetry.local_only"
	// ConfigKeyTelemetryConsent records that the consent prompt was answered.
	ConfigKeyTelemetryConsent = "telemetry.consent"
)
