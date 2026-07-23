package main

import (
	"bytes"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"

	"gitlab.com/phpboyscout/go/forge"
	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	pkgversion "gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// releaseScenarioEnv selects which in-memory release scenario the e2e binary
// exposes to the self-update subsystem. When set, applyReleaseStub injects a
// stub release source (go/forge/test) onto the tool and pins a
// deterministic current version, so `gtb update` scenarios run hermetically
// (no network). BDD scenarios set it via the "I set environment variable" step.
const releaseScenarioEnv = "GTB_E2E_RELEASE_SCENARIO"

const (
	stubCurrentVersion   = "v1.0.0"
	stubNewerVersion     = "v1.1.0"
	stubOlderVersion     = "v0.9.0"
	stubMissingTag       = "v9.9.9"
	stubNewBinaryContent = "new-binary"
)

// applyReleaseStub wires an in-memory stub release source onto the tool when
// releaseScenarioEnv is set, pinning a deterministic current version so update
// scenarios run hermetically. It is a no-op when the env var is unset, leaving
// the configured GitLab source in place.
func applyReleaseStub(p *props.Props) {
	scenario := os.Getenv(releaseScenarioEnv)
	if scenario == "" {
		return
	}

	provider, keys := buildReleaseScenario(scenario, p.Tool.Name)
	p.Tool.ReleaseProvider = provider

	if len(keys) > 0 {
		p.Tool.Signing.EmbeddedKeys = keys
	}

	p.Version = pkgversion.NewInfo(stubCurrentVersion, "", "")
}

// buildReleaseScenario maps a scenario name to a stub provider and, where the
// scenario exercises signature verification, the embedded trust key the bad
// signature is checked against. The mismatch/bad-signature scenarios abort
// before any binary swap, so they are safe to run against the shared e2e binary.
func buildReleaseScenario(scenario, toolName string) (forge.Provider, [][]byte) {
	switch scenario {
	case "already-latest":
		// Latest equals the pinned current version → update is a no-op.
		return forgetest.New(forgetest.WithRelease(stubCurrentVersion)), nil

	case "stale-latest":
		// The release source reports a "latest" OLDER than the pinned current
		// version (a stale or rolled-back listing). The downgrade guard
		// refuses before any download, so the release needs no assets.
		return forgetest.New(forgetest.WithRelease(stubOlderVersion)), nil

	case "not-found":
		// `update --version <stubMissingTag>` resolves to a missing tag.
		return forgetest.New(
			forgetest.WithRelease(stubCurrentVersion),
			forgetest.WithMissingTag(stubMissingTag),
		), nil

	case "bad-checksum":
		// A newer release whose checksums manifest hashes a different payload
		// than the served asset → checksum mismatch aborts before extract.
		asset := forgetest.TarGzAsset(toolName, toolName, stubNewBinaryContent)

		return forgetest.New(forgetest.WithRelease(stubNewerVersion,
			asset, forgetest.ChecksumsAsset(true, asset))), nil

	case "bad-signature":
		// A newer release with a good checksums manifest but a detached
		// signature over different bytes → signature verification aborts
		// before extract. The embedded trust key is the one the bad signature
		// is (unsuccessfully) checked against.
		entity := mustStubEntity()
		asset := forgetest.TarGzAsset(toolName, toolName, stubNewBinaryContent)
		manifest := forgetest.Manifest(false, asset)

		provider := forgetest.New(forgetest.WithRelease(stubNewerVersion,
			asset,
			forgetest.Asset{Name: "checksums.txt", Body: manifest},
			forgetest.SignatureAsset(entity, manifest, true),
		))

		return provider, [][]byte{armoredPublicKey(entity)}

	default:
		// Unknown scenario: a source with no releases makes GetLatestRelease
		// fail, surfacing as a clear non-zero update error.
		return forgetest.New(), nil
	}
}

// mustStubEntity generates an ephemeral Ed25519 signing entity that satisfies
// the trust-set strength policy (Ed25519/Curve25519 is accepted), so the
// bad-signature scenario reaches signature verification rather than failing on
// a weak embedded key.
func mustStubEntity() *openpgp.Entity {
	entity, err := openpgp.NewEntity("gtb-e2e", "release stub", "e2e@example.org", &packet.Config{
		Algorithm: packet.PubKeyAlgoEdDSA,
		Curve:     packet.Curve25519,
	})
	if err != nil {
		panic("e2e release stub: generate signing entity: " + err.Error())
	}

	return entity
}

// armoredPublicKey serialises an entity's public key as ASCII-armored bytes
// suitable for props.Tool.Signing.EmbeddedKeys.
func armoredPublicKey(entity *openpgp.Entity) []byte {
	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		panic("e2e release stub: armor encode: " + err.Error())
	}

	if err := entity.Serialize(w); err != nil {
		panic("e2e release stub: serialize public key: " + err.Error())
	}

	if err := w.Close(); err != nil {
		panic("e2e release stub: close armor: " + err.Error())
	}

	return buf.Bytes()
}
