package generator

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// LoadManifest reads the project's manifest, for a caller that composes the
// generator's pieces itself (gtb wizard).
func (g *Generator) LoadManifest() (*Manifest, error) {
	return g.loadManifest()
}

// SkeletonConfigFromManifest is the inverse of ManifestFromSkeletonConfig:
// the author settings a manifest records, as the config the generate path
// takes. It is what gtb wizard loads its pages from (spec 0197 D13).
func SkeletonConfigFromManifest(m Manifest) SkeletonConfig {
	return skeletonConfigFromManifest(m)
}

// ManifestFromSkeletonConfig is the pure config-to-manifest mapping the
// generate path uses, exported for the round-trip tests of its callers.
func ManifestFromSkeletonConfig(config SkeletonConfig, fileHashes map[string]string, gtbVersion string) Manifest {
	return manifestFromSkeletonConfig(config, fileHashes, gtbVersion)
}

// ApplyAuthorSettings writes a wizard's answers into an existing project: the
// author-settable fields of the manifest are replaced from cfg, everything
// the wizard does not ask (commands, hashes, the generator's recorded
// fields) is kept, the result is validated, and the derived files are synced
// (spec 0197 D13). Nothing is written when validation refuses.
func (g *Generator) ApplyAuthorSettings(ctx context.Context, cfg SkeletonConfig) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	if err := applyAuthorSettings(m, cfg); err != nil {
		return err
	}

	if err := ValidateManifest(m); err != nil {
		return err
	}

	return g.commitSetting(ctx, m)
}

// ApplyAuthorSettingsTo overlays cfg's author settings onto m without writing
// anything, for a dry run's diff. It refuses a feature ApplyAuthorSettings would.
func ApplyAuthorSettingsTo(m *Manifest, cfg SkeletonConfig) error {
	return applyAuthorSettings(m, cfg)
}

// applyAuthorSettings overlays the author-settable fields of a fresh mapping
// of cfg onto m, leaving the rest of m as it is. The fields listed here are
// the ones ManifestFromSkeletonConfig writes.
func applyAuthorSettings(m *Manifest, cfg SkeletonConfig) error {
	fresh := manifestFromSkeletonConfig(cfg, nil, "")

	if err := checkEnabledFeatureNames(m.Properties.Features, fresh.Properties.Features, m.Commands); err != nil {
		return err
	}

	m.Properties.Description = fresh.Properties.Description
	m.Properties.Features = fresh.Properties.Features
	m.Properties.EnvPrefix = fresh.Properties.EnvPrefix
	// The file-only list is edited in the manifest alone (spec 0214 D9).
	m.Properties.Config.Layers = fresh.Properties.Config.Layers
	m.Properties.Config.Formats = fresh.Properties.Config.Formats
	m.Properties.Config.Format = fresh.Properties.Config.Format
	m.Properties.Config.Sources = fresh.Properties.Config.Sources
	m.Properties.Config.KeyCase = fresh.Properties.Config.KeyCase
	m.Properties.Config.Dir = fresh.Properties.Config.Dir
	m.Properties.UpdatePolicy = fresh.Properties.UpdatePolicy
	// The withhold list is not the caller's to set (spec 0213 D6).
	m.Properties.MCP.Mode = fresh.Properties.MCP.Mode
	m.Properties.UpdateCheckInterval = fresh.Properties.UpdateCheckInterval
	m.Properties.Help = fresh.Properties.Help
	m.Properties.Telemetry = fresh.Properties.Telemetry
	m.Properties.Signing = carrySigning(m.Properties.Signing, fresh.Properties.Signing)
	m.Properties.Chat = fresh.Properties.Chat
	m.Properties.Bootstrap = fresh.Properties.Bootstrap
	m.Properties.Generator = fresh.Properties.Generator
	m.Properties.CI.ComponentSource = fresh.Properties.CI.ComponentSource
	m.Properties.ModulePath = fresh.Properties.ModulePath
	m.Properties.ForgeCredentials = fresh.Properties.ForgeCredentials
	m.ReleaseSource = fresh.ReleaseSource
	m.Version.Go = fresh.Version.Go

	return nil
}

// carrySigning is fresh with what the settings cannot express kept from
// current: the rotation overlap, a key source of "both" the settings write as
// the empty default, and the whole block while signing stays off.
func carrySigning(current, fresh ManifestSigning) ManifestSigning {
	if !fresh.Enabled {
		if !current.Enabled {
			return current
		}

		return fresh
	}

	fresh.Overlap = current.Overlap

	if fresh.KeySource == "" && current.KeySource == "both" {
		fresh.KeySource = current.KeySource
	}

	return fresh
}

// DiffAuthorSettings lists the author settings that differ between two
// manifests, one line each as `path: old -> new`, sorted by path. It is what
// `gtb wizard --dry-run` prints.
func DiffAuthorSettings(before, after *Manifest) []string {
	var out []string

	for _, s := range authorSettings {
		if s.Kind != KindSetting || settingReadOnlyReason(s.Manifest) != "" {
			continue
		}

		b, errB := manifestLeaf(before, s.Manifest)
		a, errA := manifestLeaf(after, s.Manifest)

		if errB != nil || errA != nil {
			continue
		}

		if renderLeaf(b) == renderLeaf(a) {
			continue
		}

		out = append(out, fmt.Sprintf("%s: %s -> %s", strings.TrimPrefix(s.Manifest, propertiesPrefix), renderLeaf(b), renderLeaf(a)))
	}

	sort.Strings(out)

	return out
}

// renderLeaf prints a scalar bare and a list in brackets, empty included, so
// a one-element list and a scalar read differently.
func renderLeaf(v reflect.Value) string {
	values := leafStrings(v)

	if v.Kind() == reflect.Slice {
		return "[" + strings.Join(values, ", ") + "]"
	}

	if len(values) == 0 {
		return `""`
	}

	return values[0]
}
