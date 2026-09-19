package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/release/static"
)

// On the static channel (spec 0203) the Release source check asks no
// registry: it reads the pointer and reports the tag it names, warns before
// the first release, and fails on a pointer whose manifest is missing.
func TestCheckReleaseSource_StaticChannel(t *testing.T) {
	t.Parallel()

	objects := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	base := srv.URL + "/acme/tool"
	staticProps := func() *p.Props {
		return &p.Props{Tool: p.Tool{Name: "t", ReleaseSource: p.ReleaseSource{Type: p.ReleaseSourceStatic, BaseURL: base}}}
	}

	res := checkReleaseSource(context.Background(), staticProps())
	assert.Equal(t, CheckWarn, res.Status, res.Message)
	assert.Contains(t, res.Message, "nothing published yet")
	assert.Contains(t, res.Details, base+"/latest.json")

	pointer, err := json.Marshal(static.Pointer{Schema: 1, Tool: "t", Tag: "v1.2.0", Manifest: base + "/v1.2.0/release.json", PublishedAt: "2026-09-19T12:20:00Z"})
	require.NoError(t, err)
	objects["/acme/tool/latest.json"] = pointer

	res = checkReleaseSource(context.Background(), staticProps())
	assert.Equal(t, CheckFail, res.Status, res.Message)
	assert.Contains(t, res.Message, base+"/latest.json")
	assert.Contains(t, res.Message, base+"/v1.2.0/release.json")

	manifest, err := json.Marshal(static.Manifest{
		Schema: 1, Tool: "t", Tag: "v1.2.0", ReleasedAt: "2026-09-19T12:00:00Z",
		Checksums: base + "/v1.2.0/checksums.txt", Signature: base + "/v1.2.0/checksums.txt.sig",
		Downloads: []static.Download{{OS: "linux", Arch: "amd64", Name: "t.tar.gz", URL: base + "/v1.2.0/t.tar.gz", Size: 1, SHA256: "ec90dd6d7f33fcaa8179a6647e0ad6e2fe17e0551b8989760ff2ce33ed907d81"}},
	})
	require.NoError(t, err)
	objects["/acme/tool/v1.2.0/release.json"] = manifest

	res = checkReleaseSource(context.Background(), staticProps())
	assert.Equal(t, CheckPass, res.Status, res.Message)
	assert.Contains(t, res.Message, "latest v1.2.0")
	assert.Contains(t, res.Details, "1 downloads, signed")

	// A base URL that cannot be a channel fails without a request.
	broken := staticProps()
	broken.Tool.ReleaseSource.BaseURL = "pkg.example.internal/acme"

	res = checkReleaseSource(context.Background(), broken)
	assert.Equal(t, CheckFail, res.Status)
	assert.Contains(t, res.Message, "absolute http(s) URL")
}
