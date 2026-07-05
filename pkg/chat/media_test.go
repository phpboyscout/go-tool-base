package chat

import (
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
)

// Minimal magic-byte fixtures for the stdlib sniffer.
var (
	pngBytes  = []byte("\x89PNG\r\n\x1a\n----------")
	jpegBytes = []byte("\xff\xd8\xff----------")
	gifBytes  = []byte("GIF89a----------")
	webpBytes = []byte("RIFF\x1a\x00\x00\x00WEBPVP8 ----")
	pdfBytes  = []byte("%PDF-1.7\n----------")
	mp4Bytes  = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom")
	zipBytes  = []byte("PK\x03\x04----------")
	htmlBytes = []byte("<!DOCTYPE html><html></html>")
	octet     = []byte("\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\xff\xfe\xfd")
)

func TestDetectMIME(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		data []byte
		want string
	}{
		"png":   {pngBytes, "image/png"},
		"jpeg":  {jpegBytes, "image/jpeg"},
		"gif":   {gifBytes, "image/gif"},
		"webp":  {webpBytes, "image/webp"},
		"pdf":   {pdfBytes, "application/pdf"},
		"mp4":   {mp4Bytes, "video/mp4"},
		"zip":   {zipBytes, "application/zip"},
		"html":  {htmlBytes, "text/html"},
		"octet": {octet, "application/octet-stream"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := detectMIME(tt.data); got != tt.want {
				t.Fatalf("detectMIME = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateMedia_allowsRealMedia(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{pngBytes, jpegBytes, gifBytes, webpBytes, pdfBytes} {
		rm, err := validateMedia(Media{Data: data})
		if err != nil {
			t.Fatalf("validateMedia rejected valid media: %v", err)
		}

		if rm.MIMEType == "" || len(rm.Data) == 0 {
			t.Fatalf("resolved media is empty: %+v", rm)
		}
	}
}

func TestValidateMedia_safetyRejections(t *testing.T) {
	t.Parallel()

	tests := map[string]Media{
		"empty":    {Data: nil},
		"zip":      {Data: zipBytes},
		"html":     {Data: htmlBytes},
		"octet":    {Data: octet},
		"oversize": {Data: make([]byte, maxMediaBytes+1)},
		"mislabel": {MIMEType: "image/png", Data: zipBytes}, // declared image, is a zip
	}

	for name, m := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := validateMedia(m); !errors.Is(err, ErrMediaRejected) {
				t.Fatalf("want ErrMediaRejected, got %v", err)
			}
		})
	}
}

// A benign subtype mismatch within the same family is allowed, and the SNIFFED
// type wins (a jpeg-declared png is sent as png).
func TestValidateMedia_subtypeMismatchAllowed_sniffWins(t *testing.T) {
	t.Parallel()

	rm, err := validateMedia(Media{MIMEType: "image/jpeg", Data: pngBytes})
	if err != nil {
		t.Fatalf("same-family mismatch should be allowed: %v", err)
	}

	if rm.MIMEType != "image/png" {
		t.Fatalf("sniffed type should win: got %q, want image/png", rm.MIMEType)
	}
}

func TestValidateMediaSet_providerCapability(t *testing.T) {
	t.Parallel()

	img := Media{Data: pngBytes}

	// Gemini is wired first and accepts images, PDF, and A/V uniformly.
	for _, m := range []Media{img, {Data: pdfBytes}, {Data: mp4Bytes}} {
		if _, err := validateMediaSet(ProviderGemini, []Media{m}); err != nil {
			t.Fatalf("gemini should accept %v: %v", detectMIME(m.Data), err)
		}
	}

	// Claude accepts images but not video/PDF in v1.
	if _, err := validateMediaSet(ProviderClaude, []Media{img}); err != nil {
		t.Fatalf("claude should accept an image: %v", err)
	}

	if _, err := validateMediaSet(ProviderClaude, []Media{{Data: mp4Bytes}}); !errors.Is(err, ErrMediaUnsupported) {
		t.Fatalf("claude should not accept video in v1: %v", err)
	}

	// Providers not yet wired (or that never accept media) reject it rather than
	// silently dropping — they join mediaSupport when their mapping lands.
	for _, p := range []Provider{ProviderOpenAI, ProviderClaudeLocal} {
		if _, err := validateMediaSet(p, []Media{img}); !errors.Is(err, ErrMediaUnsupported) {
			t.Fatalf("%s should reject media until wired: %v", p, err)
		}
	}
}

func TestValidateMediaSet_countLimitAndEmpty(t *testing.T) {
	t.Parallel()

	if got, err := validateMediaSet(ProviderGemini, nil); err != nil || got != nil {
		t.Fatalf("no media should be a no-op: got %v err %v", got, err)
	}

	many := make([]Media, maxMediaCount+1)
	for i := range many {
		many[i] = Media{Data: pngBytes}
	}

	if _, err := validateMediaSet(ProviderGemini, many); !errors.Is(err, ErrMediaRejected) {
		t.Fatalf("over-count should be rejected: %v", err)
	}
}

// The attachment index is surfaced in the error for debuggability.
func TestValidateMediaSet_reportsIndex(t *testing.T) {
	t.Parallel()

	media := []Media{{Data: pngBytes}, {Data: zipBytes}}

	_, err := validateMediaSet(ProviderGemini, media)
	if err == nil || !strings.Contains(err.Error(), "attachment 1") {
		t.Fatalf("error should name attachment 1: %v", err)
	}
}
