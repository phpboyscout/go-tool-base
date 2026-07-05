package chat

import (
	"net/http"
	"strings"

	"github.com/cockroachdb/errors"
)

// Media is one input attachment (an image, PDF, or supported A/V clip) sent
// alongside a text prompt to a multimodal model (spec 2026-07-05-chat-multimodal).
// Data holds the raw bytes. MIMEType is an OPTIONAL cross-check: the type is always
// sniffed from Data and the sniffed type is authoritative (and what is sent); when
// MIMEType is set it must match the sniffed family or the attachment is rejected,
// so a declared type can never smuggle disguised content past the safety filter.
type Media struct {
	MIMEType string
	Data     []byte
}

// ErrMediaRejected is returned when an attachment fails the safety filter — empty,
// too large, an unidentifiable/disallowed content type, or a declared type that
// contradicts the sniffed bytes. Wrapped with detail; test with errors.Is.
var ErrMediaRejected = errors.New("media rejected")

// ErrMediaUnsupported is returned when the selected provider (or model) cannot
// accept media, or cannot accept a particular attachment's type.
var ErrMediaUnsupported = errors.New("media not supported by provider")

const (
	// maxMediaBytes caps a single attachment. Conservative across providers; a
	// larger request should downscale first.
	maxMediaBytes = 20 << 20 // 20 MiB
	// maxMediaCount caps attachments per request.
	maxMediaCount = 16
)

// The v1 media allowlist: the types stdlib net/http.DetectContentType can
// positively identify AND at least one provider accepts. Grouped for the
// per-provider support map; the union is the allowlist. Long-tail formats stdlib
// cannot name (mov, flv, wmv, 3gpp, flac, m4a) are a fast-follow behind a richer
// sniffer (spec Q2/Q3).
var (
	imageMediaTypes = []string{"image/jpeg", "image/png", "image/gif", "image/webp"}
	docMediaTypes   = []string{"application/pdf"}
	videoMediaTypes = []string{"video/mp4", "video/webm", "video/avi"}
	audioMediaTypes = []string{"audio/mpeg", "audio/wave", "audio/aiff", "application/ogg"}
)

// mediaSupport maps a provider to the media types it accepts (spec §6). A provider
// absent from the map (e.g. ProviderClaudeLocal) accepts no media. Entries are
// added as each provider's media mapping lands, so a provider is only listed once
// its attachments are actually wired — never a silent drop. Gemini's genai Part
// carries any media type uniformly; Claude and OpenAI (images) join in their steps.
var mediaSupport = map[Provider]map[string]bool{
	ProviderGemini: set(imageMediaTypes, docMediaTypes, videoMediaTypes, audioMediaTypes),
	ProviderClaude: set(imageMediaTypes, docMediaTypes),
	ProviderOpenAI: set(imageMediaTypes, docMediaTypes),
}

// mimePDF is the one document type in the v1 allowlist; it maps to a provider's
// document/file block rather than an image block.
const mimePDF = "application/pdf"

// allowedMediaTypes is the union of every provider's accepted types — the v1
// allowlist a sniffed type must be on to be sendable to anyone.
var allowedMediaTypes = union(mediaSupport)

// set builds a lookup set from one or more type groups.
func set(groups ...[]string) map[string]bool {
	m := map[string]bool{}

	for _, g := range groups {
		for _, t := range g {
			m[t] = true
		}
	}

	return m
}

// union collects every type any provider supports.
func union(support map[Provider]map[string]bool) map[string]bool {
	m := map[string]bool{}

	for _, types := range support {
		for t := range types {
			m[t] = true
		}
	}

	return m
}

// resolvedMedia is an attachment that has passed the safety filter: its type is
// known, allowlisted, and provider-accepted, and it is ready to map to a provider
// request. Providers consume []resolvedMedia, never raw Media.
type resolvedMedia struct {
	MIMEType string
	Data     []byte
}

// detectMIME sniffs the content type from the bytes (stdlib "mime magic"),
// stripping any parameters (e.g. "text/plain; charset=utf-8" → "text/plain").
func detectMIME(data []byte) string {
	ct := http.DetectContentType(data)
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}

	return strings.ToLower(strings.TrimSpace(ct))
}

// mediaFamily is the top-level type (before the "/"), used for the declared-vs-
// sniffed cross-check: a declared image that sniffs as an application is a gross
// mismatch (smuggling); a declared image/jpeg that sniffs as image/png is not.
func mediaFamily(mime string) string {
	if i := strings.IndexByte(mime, '/'); i >= 0 {
		return mime[:i]
	}

	return mime
}

// providerSupportsMedia reports whether the provider accepts any media at all.
func providerSupportsMedia(p Provider) bool {
	return len(mediaSupport[p]) > 0
}

// validateMediaSet runs the safety choke point over a request's attachments before
// any network call (spec §5): it enforces the provider's media capability, the
// per-attachment sniff + declared cross-check + allowlist, the provider's
// type support, and the size/count caps. It returns the resolved, ready-to-send
// attachments or a typed error.
func validateMediaSet(provider Provider, media []Media) ([]resolvedMedia, error) {
	if len(media) == 0 {
		return nil, nil
	}

	if !providerSupportsMedia(provider) {
		return nil, errors.Wrapf(ErrMediaUnsupported, "provider %q accepts no media", provider)
	}

	if len(media) > maxMediaCount {
		return nil, errors.Wrapf(ErrMediaRejected, "%d attachments exceeds the limit of %d", len(media), maxMediaCount)
	}

	out := make([]resolvedMedia, 0, len(media))

	for i, m := range media {
		rm, err := validateMedia(m)
		if err != nil {
			return nil, errors.Wrapf(err, "attachment %d", i)
		}

		if !mediaSupport[provider][rm.MIMEType] {
			return nil, errors.Wrapf(ErrMediaUnsupported, "provider %q does not accept %q", provider, rm.MIMEType)
		}

		out = append(out, rm)
	}

	return out, nil
}

// validateMedia validates a single attachment: non-empty, within the size cap,
// a sniffed type on the allowlist, and — if declared — a family that matches the
// sniff. The sniffed type is authoritative and returned as the send type.
func validateMedia(m Media) (resolvedMedia, error) {
	switch {
	case len(m.Data) == 0:
		return resolvedMedia{}, errors.Wrap(ErrMediaRejected, "empty attachment")
	case len(m.Data) > maxMediaBytes:
		return resolvedMedia{}, errors.Wrapf(ErrMediaRejected,
			"%d bytes exceeds the per-attachment limit of %d", len(m.Data), maxMediaBytes)
	}

	sniffed := detectMIME(m.Data)

	if !allowedMediaTypes[sniffed] {
		return resolvedMedia{}, errors.Wrapf(ErrMediaRejected,
			"unidentifiable or disallowed content type %q", sniffed)
	}

	if m.MIMEType != "" {
		declared := strings.ToLower(strings.TrimSpace(m.MIMEType))
		if mediaFamily(declared) != mediaFamily(sniffed) {
			return resolvedMedia{}, errors.Wrapf(ErrMediaRejected,
				"declared %q but content sniffs as %q", declared, sniffed)
		}
	}

	return resolvedMedia{MIMEType: sniffed, Data: m.Data}, nil
}
