package steps_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cucumber/godog"

	forgetest "gitlab.com/phpboyscout/go/forge/test"
	"gitlab.com/phpboyscout/go/signing/minisign"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// The variables the e2e binary reads its trust from (cli/cmd/e2e/release_stub.go).
const (
	staticReleaseKeyEnv = "GTB_E2E_STATIC_RELEASE_KEY"
	staticAuthorityEnv  = "GTB_E2E_STATIC_ROTATION_AUTHORITY"
)

type rotationChannelKey struct{}

// rotationChannel is a static channel whose binary trusts a release key that
// has been compromised, with a rotation authority that has signed a manifest
// moving trust to a replacement (spec 0101). Each release it publishes serves
// the scenario's own binary, so an update leaves a binary that runs, and
// still reports the stub's v1.0.0, ready to update again.
type rotationChannel struct {
	server      *httptest.Server
	objects     map[string][]byte
	base        string
	binary      string
	compromised *openpgp.Entity
	replacement *openpgp.Entity
	rotation    []byte
	rotationSig []byte
}

func initRotationSteps(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if ch, ok := ctx.Value(rotationChannelKey{}).(*rotationChannel); ok {
			ch.server.Close()
		}

		return ctx, nil
	})

	ctx.Step(`^a static release channel whose release key was compromised$`, aStaticChannelWhoseReleaseKeyWasCompromised)
	ctx.Step(`^the channel publishes "([^"]*)" signed by the (compromised|replacement) key( with the rotation manifest)?$`, theChannelPublishes)
}

// aStaticChannelWhoseReleaseKeyWasCompromised gives the scenario its own copy
// of the binary, because a successful update replaces it, and points that
// copy at an empty channel and at the compromised key.
func aStaticChannelWhoseReleaseKeyWasCompromised(ctx context.Context) (context.Context, error) {
	w := getCLIWorld(ctx)

	binary, err := os.ReadFile(w.binaryPath)
	if err != nil {
		return ctx, err
	}

	ch := &rotationChannel{objects: map[string][]byte{}, binary: string(binary)}

	if ch.compromised, err = newReleaseEntity("Compromised Release Key"); err != nil {
		return ctx, err
	}

	if ch.replacement, err = newReleaseEntity("Replacement Release Key"); err != nil {
		return ctx, err
	}

	compromisedPub, err := armoredPublicKey(ch.compromised)
	if err != nil {
		return ctx, err
	}

	authority, err := ch.signCompromiseManifest()
	if err != nil {
		return ctx, err
	}

	ch.server = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		body, ok := ch.objects[r.URL.Path]
		if !ok {
			http.NotFound(rw, r)

			return
		}

		_, _ = rw.Write(body)
	}))
	ch.base = ch.server.URL + "/acme/gtb"

	if err := writeOwnBinary(w.configDir, binary); err != nil {
		return ctx, err
	}

	w.binaryPath = filepath.Join(w.configDir, "bin", "gtb")

	if w.envVars == nil {
		w.envVars = make(map[string]string)
	}

	w.envVars[staticChannelEnv] = ch.base
	w.envVars[staticReleaseKeyEnv] = compromisedPub
	w.envVars[staticAuthorityEnv] = authority

	return context.WithValue(ctx, rotationChannelKey{}, ch), nil
}

// signCompromiseManifest adds the replacement key and revokes the compromised
// one, signed by a fresh rotation authority whose public key it returns.
func (ch *rotationChannel) signCompromiseManifest() (string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}

	authority, err := minisign.NewPublicKey(pub)
	if err != nil {
		return "", err
	}

	compromised, err := fingerprint(ch.compromised)
	if err != nil {
		return "", err
	}

	replacementPub, err := armoredPublicKey(ch.replacement)
	if err != nil {
		return "", err
	}

	replacement, err := fingerprint(ch.replacement)
	if err != nil {
		return "", err
	}

	now := time.Now()
	ch.rotation, ch.rotationSig, err = verify.SignRotationManifest(priv, verify.RotationManifest{
		Schema:   verify.RotationSchema,
		IssuedAt: now.Add(-time.Minute),
		NotAfter: now.Add(24 * time.Hour),
		Reason:   verify.RotationReasonCompromise,
		Add:      []verify.RotationAdd{{Fingerprint: replacement, Armored: replacementPub}},
		Revoke:   []verify.RotationRevoke{{Fingerprint: compromised, Since: now.Add(-time.Hour)}},
		Epoch:    1,
	})

	return authority.Encode(), err
}

// theChannelPublishes puts a release on the channel and moves the pointer to
// it. Its checksums are signed by the named key, and it carries the
// compromise manifest when asked to, as every release after a revoke should.
func theChannelPublishes(ctx context.Context, tag, signer, withManifest string) (context.Context, error) {
	ch, ok := ctx.Value(rotationChannelKey{}).(*rotationChannel)
	if !ok {
		return ctx, fmt.Errorf("no rotation channel in this scenario")
	}

	key := ch.compromised
	if signer == "replacement" {
		key = ch.replacement
	}

	archive := forgetest.TarGzAsset("gtb", "gtb", ch.binary)
	checksums := forgetest.ChecksumsAsset(false, archive).Body

	var sig bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sig, key, bytes.NewReader(checksums), nil); err != nil {
		return ctx, err
	}

	prefix := "/acme/gtb/" + tag + "/"
	ch.objects[prefix+archive.Name] = archive.Body
	ch.objects[prefix+static.ChecksumsFile] = checksums
	ch.objects[prefix+static.SignatureFile] = sig.Bytes()

	sum := sha256.Sum256(archive.Body)
	m := static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       "gtb",
		Tag:        tag,
		ReleasedAt: time.Now().UTC().Format(time.RFC3339),
		Checksums:  static.FileURL(ch.base, tag, static.ChecksumsFile),
		Signature:  static.FileURL(ch.base, tag, static.SignatureFile),
		Downloads: []static.Download{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Name: archive.Name,
			URL: static.FileURL(ch.base, tag, archive.Name), Size: int64(len(archive.Body)), SHA256: hex.EncodeToString(sum[:]),
		}},
	}

	if withManifest != "" {
		ch.objects[prefix+static.RotationFile] = ch.rotation
		ch.objects[prefix+static.RotationSignatureFile] = ch.rotationSig
		m.Rotation = static.FileURL(ch.base, tag, static.RotationFile)
		m.RotationSignature = static.FileURL(ch.base, tag, static.RotationSignatureFile)
	}

	manifest, err := json.Marshal(m)
	if err != nil {
		return ctx, err
	}

	ch.objects[prefix+static.ManifestFile] = manifest

	pointer, err := json.Marshal(static.Pointer{
		Schema: static.SchemaVersion, Tool: "gtb", Tag: tag,
		Manifest: static.ManifestURL(ch.base, tag), PublishedAt: time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return ctx, err
	}

	ch.objects["/acme/gtb/"+static.PointerFile] = pointer

	return ctx, nil
}

// writeOwnBinary writes the scenario's copy of the binary as bin/gtb under dir.
func writeOwnBinary(dir string, binary []byte) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	if err := root.MkdirAll("bin", 0o755); err != nil {
		return err
	}

	return root.WriteFile("bin/gtb", binary, 0o700)
}

func newReleaseEntity(name string) (*openpgp.Entity, error) {
	return openpgp.NewEntity(name, "", "release@test.example", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Curve: packet.Curve25519})
}

func armoredPublicKey(e *openpgp.Entity) (string, error) {
	var buf bytes.Buffer

	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}

	if err := e.Serialize(w); err != nil {
		return "", err
	}

	if err := w.Close(); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func fingerprint(e *openpgp.Entity) (string, error) {
	pub, err := armoredPublicKey(e)
	if err != nil {
		return "", err
	}

	ts, err := verify.LoadTrustSet([]byte(pub))
	if err != nil {
		return "", err
	}

	return ts.Fingerprints()[0], nil
}
