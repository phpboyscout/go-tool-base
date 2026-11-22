package setup

import (
	"net/http"
	"strings"

	"gitlab.com/phpboyscout/go/httpclient"
	transithttp "gitlab.com/phpboyscout/go/transit/http"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ReleaseHTTPClient is the client a tool reads its own release channel with.
// Its requests carry a User-Agent of "<tool>/<version>" and nothing more, so a
// release store can count update checks per release (go-tool-base#157).
func ReleaseHTTPClient(p *props.Props) *http.Client {
	ua := releaseUserAgent(p)
	if ua == "" {
		return httpclient.NewClient()
	}

	return httpclient.NewClient(httpclient.WithClientMiddleware(transithttp.NewClientChain(withUserAgent(ua))))
}

func releaseUserAgent(p *props.Props) string {
	if p == nil || p.Tool.Name == "" {
		return ""
	}

	ua := headerToken(p.Tool.Name)
	if v := p.GetVersion().GetVersion(); v != "" {
		ua += "/" + headerToken(v)
	}

	return ua
}

func withUserAgent(ua string) transithttp.ClientMiddleware {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("User-Agent", ua)

			return next.RoundTrip(req)
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// headerToken replaces what an RFC 9110 token may not hold, because a version
// stamped by ldflags is not validated and a bad header fails the request.
func headerToken(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x80 && (r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return r
		}

		return '-'
	}, s)
}
