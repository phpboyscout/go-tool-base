package steps_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"

	"github.com/cucumber/godog"

	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// staticChannelEnv is the variable the e2e binary reads to join a static
// release channel (cli/cmd/e2e/release_stub.go). The step below serves one
// from this test process and hands its URL over through it.
const staticChannelEnv = "GTB_E2E_STATIC_CHANNEL"

// The pinned versions the e2e binary reports, mirrored from release_stub.go.
const (
	staticCurrentVersion = "v1.0.0"
	staticNewerVersion   = "v1.1.0"
	staticOlderVersion   = "v0.9.0"
)

type staticChannelKey struct{}

// staticChannel is one scenario's channel: a server whose objects are the
// pointer, the manifests and the files they name, laid out as the layout
// reference says. It closes with the scenario.
type staticChannel struct {
	server  *httptest.Server
	objects map[string][]byte
}

func initStaticChannelSteps(ctx *godog.ScenarioContext) {
	ctx.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		if ch, ok := ctx.Value(staticChannelKey{}).(*staticChannel); ok {
			ch.server.Close()
		}

		return ctx, nil
	})

	ctx.Step(`^a static release channel serving "([^"]*)"$`, aStaticReleaseChannelServing)
}

// aStaticReleaseChannelServing starts the channel for a named scenario and
// points the binary at it. The scenarios mirror the forge double's, so the
// two branches of the updater are proven against the same outcomes.
func aStaticReleaseChannelServing(ctx context.Context, scenario string) (context.Context, error) {
	ch := &staticChannel{objects: map[string][]byte{}}
	ch.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := ch.objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(body)
	}))

	base := ch.server.URL + "/acme/gtb"

	if err := ch.serve(base, scenario); err != nil {
		ch.server.Close()

		return ctx, err
	}

	w := getCLIWorld(ctx)
	if w.envVars == nil {
		w.envVars = make(map[string]string)
	}

	w.envVars[staticChannelEnv] = base

	return context.WithValue(ctx, staticChannelKey{}, ch), nil
}

// serve lays out the named scenario's releases. "nothing-published" is a
// channel with no pointer yet; "chain" is two releases with the pointer on
// the newer.
func (ch *staticChannel) serve(base, scenario string) error {
	type release struct {
		tag, previous   string
		corruptChecksum bool
	}

	scenarios := map[string][]release{
		"nothing-published": {},
		"already-latest":    {{tag: staticCurrentVersion}},
		"stale-latest":      {{tag: staticOlderVersion}},
		"bad-checksum":      {{tag: staticNewerVersion, previous: staticCurrentVersion, corruptChecksum: true}},
		"chain":             {{tag: staticCurrentVersion}, {tag: staticNewerVersion, previous: staticCurrentVersion}},
	}

	releases, ok := scenarios[scenario]
	if !ok {
		return fmt.Errorf("unknown static channel scenario %q", scenario)
	}

	for _, r := range releases {
		if err := ch.publish(base, r.tag, r.previous, r.corruptChecksum); err != nil {
			return err
		}
	}

	return nil
}

// publish puts one release on the channel for this platform and moves the
// pointer to it. corruptChecksum serves a checksums file that hashes other
// bytes, so the archive fails verification after it downloads.
func (ch *staticChannel) publish(base, tag, previous string, corruptChecksum bool) error {
	archive := forgetest.TarGzAsset("gtb", "gtb", "new-binary")
	prefix := "/acme/gtb/" + tag + "/"

	ch.objects[prefix+archive.Name] = archive.Body
	ch.objects[prefix+static.ChecksumsFile] = forgetest.ChecksumsAsset(corruptChecksum, archive).Body

	sum := sha256.Sum256(archive.Body)
	manifest, err := json.Marshal(static.Manifest{
		Schema:     static.SchemaVersion,
		Tool:       "gtb",
		Tag:        tag,
		ReleasedAt: "2026-09-19T12:00:00Z",
		Previous:   previous,
		Checksums:  static.FileURL(base, tag, static.ChecksumsFile),
		Downloads: []static.Download{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, Name: archive.Name,
			URL: static.FileURL(base, tag, archive.Name), Size: int64(len(archive.Body)), SHA256: hex.EncodeToString(sum[:]),
		}},
	})
	if err != nil {
		return err
	}

	ch.objects[prefix+static.ManifestFile] = manifest

	pointer, err := json.Marshal(static.Pointer{
		Schema: static.SchemaVersion, Tool: "gtb", Tag: tag,
		Manifest: static.ManifestURL(base, tag), PublishedAt: "2026-09-19T12:20:00Z",
	})
	if err != nil {
		return err
	}

	ch.objects["/acme/gtb/"+static.PointerFile] = pointer

	return nil
}
