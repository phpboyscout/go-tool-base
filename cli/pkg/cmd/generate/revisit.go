package generate

import (
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// optionsFromManifest loads a project's manifest into the wizard's options,
// marked as a revisit: the inverse of skeletonConfig, kept beside it so the
// two cannot drift (spec 0197 D13). TestOptionsFromManifest_RoundTrip holds
// them together through the manifest.
func optionsFromManifest(m generator.Manifest) *SkeletonOptions {
	cfg := generator.SkeletonConfigFromManifest(m)

	o := &SkeletonOptions{
		Name:                  cfg.Name,
		Description:           cfg.Description,
		GoVersion:             cfg.GoVersion,
		Features:              selectedFeatureNames(cfg.Features),
		Repo:                  cfg.Repo,
		Host:                  cfg.Host,
		Private:               cfg.Private,
		ForgeBackend:          string(cfg.ForgeBackend),
		Module:                cfg.ModulePath,
		ReleaseChannel:        cfg.ReleaseChannel,
		Direct:                cfg.Direct,
		HelpType:              cfg.HelpType,
		SlackChannel:          cfg.SlackChannel,
		SlackTeam:             cfg.SlackTeam,
		TeamsChannel:          cfg.TeamsChannel,
		TeamsTeam:             cfg.TeamsTeam,
		EnvPrefix:             cfg.EnvPrefix,
		UpdatePolicy:          cfg.UpdatePolicy,
		UpdateCheckInterval:   cfg.UpdateCheckInterval,
		TelemetryEndpoint:     cfg.TelemetryEndpoint,
		TelemetryOTelEndpoint: cfg.TelemetryOTelEndpoint,
		Bootstrap:             cfg.Bootstrap,
		ConfigLayers:          cfg.ConfigLayers,
		CIComponentSource:     cfg.CIComponentSource,
		ChatProviders:         cfg.Chat.Providers,
		ChatDefault:           cfg.Chat.Default,
		revisit:               true,
	}

	o.hosted = cfg.ForgeBackend != ""
	o.NoForge = !o.hosted

	if o.hosted {
		// A hosted project derives its module path; the wizard asks it only
		// when not hosted.
		o.Module = ""
	}

	if o.HelpType == "" {
		o.HelpType = "none"
	}

	for _, id := range cfg.ForgeCredentials {
		o.ForgeCredentials = append(o.ForgeCredentials, string(id))
	}

	o.Signing = cfg.Signing.Enabled
	o.SigningEmail = cfg.Signing.ExternalKeyEmail
	o.SigningKeySource = cfg.Signing.KeySource
	o.SigningKeyID = cfg.Signing.KeyID
	o.SigningBackend = cfg.Signing.Backend
	o.SigningKMSRegion = cfg.Signing.KMSRegion
	o.SigningPublicKey = cfg.Signing.PublicKey
	o.SigningRequireExternalCrosscheck = cfg.Signing.RequireExternalCrosscheck
	o.SigningRequireSignature = cfg.Signing.RequireSignature
	o.SigningRequireChecksum = cfg.Signing.RequireChecksum

	return o
}

// selectedFeatureNames is the wizard's feature list (the selectable ones that
// are effectively enabled) from a manifest's delta-normalised entries. The
// forges are implied by the backend and never selected.
func selectedFeatureNames(features []generator.ManifestFeature) []string {
	var out []string

	for _, name := range generator.SelectableFeatures {
		if generator.FeatureEnabledIn(features, name) {
			out = append(out, name)
		}
	}

	return out
}
