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
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/cucumber/godog"

	forgetest "gitlab.com/phpboyscout/go/forge/test"
	"gitlab.com/phpboyscout/go/signing/minisign"
	"gitlab.com/phpboyscout/go/signing/verify"

	"gitlab.com/phpboyscout/go-tool-base/internal/binversion/binversiontest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// The variables the e2e binary reads its trust from (cli/cmd/e2e/release_stub.go).
const (
	staticReleaseKeyEnv = "GTB_E2E_STATIC_RELEASE_KEY"
	staticAuthorityEnv  = "GTB_E2E_STATIC_ROTATION_AUTHORITY"
)

// forgeChannelEnv and forgeTypeEnv point the e2e binary at a local forge.
const (
	forgeChannelEnv = "GTB_E2E_FORGE_CHANNEL"
	forgeTypeEnv    = "GTB_E2E_FORGE_TYPE"
)

const staticKind = "static"

type rotationChannelKey struct{}

// rotationChannel is a release channel, static or a forge's, whose binary trusts a release key that
// has been compromised, with a rotation authority that has signed a manifest
// moving trust to a replacement (spec 0101). Each release it publishes serves
// a binary of that version, and puts back the scenario's own, which reports
// the stub's v1.0.0 and is ready to update again.
type rotationChannel struct {
	kind        string
	releases    []giteaRelease
	server      *httptest.Server
	objectsMu   sync.RWMutex // the server reads objects while a step publishes
	objects     map[string][]byte
	base        string
	binary      []byte
	compromised *openpgp.Entity
	replacement *openpgp.Entity
	rotation    []byte
	rotationSig []byte
}

func (ch *rotationChannel) object(path string) ([]byte, bool) {
	ch.objectsMu.RLock()
	defer ch.objectsMu.RUnlock()

	body, ok := ch.objects[path]

	return body, ok
}

func (ch *rotationChannel) putObject(path string, body []byte) {
	ch.objectsMu.Lock()
	defer ch.objectsMu.Unlock()

	ch.objects[path] = body
}

func initRotationSteps(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if ch, ok := ctx.Value(rotationChannelKey{}).(*rotationChannel); ok {
			ch.server.Close()
		}

		return ctx, nil
	})

	ctx.Step(`^a (static|gitea|codeberg) release channel whose release key was compromised$`, aChannelWhoseReleaseKeyWasCompromised)
	ctx.Step(`^the home directory is a git checkout$`, theHomeDirectoryIsAGitCheckout)
	ctx.Step(`^the channel publishes "([^"]*)" signed by the (compromised|replacement) key( with the rotation manifest)?$`, theChannelPublishes)
}

// aChannelWhoseReleaseKeyWasCompromised gives the scenario its own copy
// of the binary, because a successful update replaces it, and points that
// copy at an empty channel and at the compromised key.
func aChannelWhoseReleaseKeyWasCompromised(ctx context.Context, kind string) (context.Context, error) {
	w := getCLIWorld(ctx)

	binary, err := ownBinary(w)
	if err != nil {
		return ctx, err
	}

	ch := &rotationChannel{kind: kind, objects: map[string][]byte{}, binary: binary}

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
		body, ok := ch.object(r.URL.Path)
		if !ok {
			http.NotFound(rw, r)

			return
		}

		_, _ = rw.Write(body)
	}))
	ch.base = ch.server.URL + "/acme/gtb"

	if w.envVars == nil {
		w.envVars = make(map[string]string)
	}

	if kind == staticKind {
		w.envVars[staticChannelEnv] = ch.base
	} else {
		w.envVars[forgeChannelEnv] = ch.server.URL
		w.envVars[forgeTypeEnv] = kind
	}

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

// theHomeDirectoryIsAGitCheckout relies on each run taking the config directory as HOME.
func theHomeDirectoryIsAGitCheckout(ctx context.Context) error {
	return os.Mkdir(filepath.Join(getCLIWorld(ctx).configDir, ".git"), 0o700)
}

// releaseFile is one file a release carries.
type releaseFile struct {
	name string
	body []byte
}

// theChannelPublishes makes a release the channel's latest. Its checksums are
// signed by the named key, and it carries the compromise manifest when asked
// to, as every release after a revoke should.
func theChannelPublishes(ctx context.Context, tag, signer, withManifest string) (context.Context, error) {
	ch, ok := ctx.Value(rotationChannelKey{}).(*rotationChannel)
	if !ok {
		return ctx, fmt.Errorf("no rotation channel in this scenario")
	}

	key := ch.compromised
	if signer == "replacement" {
		key = ch.replacement
	}

	if err := writeOwnBinary(getCLIWorld(ctx).configDir, ch.binary); err != nil {
		return ctx, err
	}

	released, err := binversiontest.Build(ctx, tag, binversiontest.Tagged)
	if err != nil {
		return ctx, err
	}

	archive := forgetest.TarGzAsset("gtb", "gtb", string(released))
	checksums := forgetest.ChecksumsAsset(false, archive).Body

	var sig bytes.Buffer
	if err := openpgp.ArmoredDetachSign(&sig, key, bytes.NewReader(checksums), nil); err != nil {
		return ctx, err
	}

	files := []releaseFile{
		{archive.Name, archive.Body},
		{static.ChecksumsFile, checksums},
		{static.SignatureFile, sig.Bytes()},
	}

	if withManifest != "" {
		files = append(files, releaseFile{static.RotationFile, ch.rotation}, releaseFile{static.RotationSignatureFile, ch.rotationSig})
	}

	if ch.kind == staticKind {
		return ctx, ch.publishStatic(tag, files, withManifest != "")
	}

	return ctx, ch.publishForge(tag, files)
}

// publishStatic writes a release's files and manifest under its tag, and moves
// the pointer to it. The archive is files' first.
func (ch *rotationChannel) publishStatic(tag string, files []releaseFile, withManifest bool) error {
	prefix := "/acme/gtb/" + tag + "/"
	for _, f := range files {
		ch.putObject(prefix+f.name, f.body)
	}

	archive := files[0]
	sum := sha256.Sum256(archive.body)
	m := static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       "gtb",
		Tag:        tag,
		ReleasedAt: time.Now().UTC().Format(time.RFC3339),
		Checksums:  static.FileURL(ch.base, tag, static.ChecksumsFile),
		Signature:  static.FileURL(ch.base, tag, static.SignatureFile),
		Downloads: []static.Download{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Name: archive.name,
			URL: static.FileURL(ch.base, tag, archive.name), Size: int64(len(archive.body)), SHA256: hex.EncodeToString(sum[:]),
		}},
	}

	if withManifest {
		m.Rotation = static.FileURL(ch.base, tag, static.RotationFile)
		m.RotationSignature = static.FileURL(ch.base, tag, static.RotationSignatureFile)
	}

	if err := ch.putJSON(prefix+static.ManifestFile, m); err != nil {
		return err
	}

	return ch.putJSON("/acme/gtb/"+static.PointerFile, static.Pointer{
		Schema: static.SchemaVersion, Tool: "gtb", Tag: tag,
		Manifest: static.ManifestURL(ch.base, tag), PublishedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// giteaRelease and giteaAttachment are the fields an update reads, as Gitea
// 1.25 answered for a release goreleaser published (#221).
type giteaRelease struct {
	ID          int64             `json:"id"`
	TagName     string            `json:"tag_name"`
	Name        string            `json:"name"`
	Body        string            `json:"body"`
	Draft       bool              `json:"draft"`
	Prerelease  bool              `json:"prerelease"`
	CreatedAt   time.Time         `json:"created_at"`
	PublishedAt time.Time         `json:"published_at"`
	Assets      []giteaAttachment `json:"assets"`
}

type giteaAttachment struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"browser_download_url"`
}

// publishForge attaches a release's files and makes it the repository's latest.
func (ch *rotationChannel) publishForge(tag string, files []releaseFile) error {
	now := time.Now().UTC()
	rel := giteaRelease{ID: int64(len(ch.releases) + 1), TagName: tag, Name: tag, CreatedAt: now, PublishedAt: now}

	for i, f := range files {
		path := "/acme/gtb/releases/download/" + tag + "/" + f.name
		ch.putObject(path, f.body)
		rel.Assets = append(rel.Assets, giteaAttachment{
			ID: int64(i + 1), Name: f.name, Size: int64(len(f.body)), DownloadURL: ch.server.URL + path,
		})
	}

	ch.releases = append([]giteaRelease{rel}, ch.releases...)

	const api = "/api/v1/repos/acme/gtb/releases"

	for path, doc := range map[string]any{
		"/api/v1/version":    map[string]string{"version": "1.25.5"},
		api:                  ch.releases,
		api + "/latest":      rel,
		api + "/tags/" + tag: rel,
	} {
		if err := ch.putJSON(path, doc); err != nil {
			return err
		}
	}

	return nil
}

func (ch *rotationChannel) putJSON(path string, doc any) error {
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	ch.putObject(path, body)

	return nil
}

// ownBinary points the scenario at a copy of the binary, which it returns,
// because a successful update replaces it.
func ownBinary(w *cliWorld) ([]byte, error) {
	binary, err := os.ReadFile(w.binaryPath)
	if err != nil {
		return nil, err
	}

	if err := writeOwnBinary(w.configDir, binary); err != nil {
		return nil, err
	}

	w.binaryPath = filepath.Join(w.configDir, "bin", "gtb")

	return binary, nil
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

	// A rename, because the copy already there may still be running.
	if err := root.WriteFile("bin/gtb.new", binary, 0o700); err != nil {
		return err
	}

	return root.Rename("bin/gtb.new", "bin/gtb")
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
