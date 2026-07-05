package chat

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// rm is a resolved (already safety-checked) attachment for the builder tests.
func rm(mime string, data []byte) resolvedMedia { return resolvedMedia{MIMEType: mime, Data: data} }

// Gemini: media maps to inline-blob parts appended after the text, in order.
func TestGeminiParts_textThenMedia(t *testing.T) {
	t.Parallel()

	data := []byte("\xff\xd8\xffjpeg-bytes")

	parts := geminiParts("describe this", []resolvedMedia{rm("image/jpeg", data)})
	if len(parts) != 2 {
		t.Fatalf("want text + 1 media part, got %d", len(parts))
	}

	if parts[0].Text != "describe this" {
		t.Fatalf("first part should be the prompt text, got %q", parts[0].Text)
	}

	if parts[1].InlineData == nil || parts[1].InlineData.MIMEType != "image/jpeg" {
		t.Fatalf("second part should be the image blob, got %+v", parts[1])
	}

	if !bytes.Equal(parts[1].InlineData.Data, data) {
		t.Fatal("blob data does not match the attachment bytes")
	}
}

// The text path is unchanged: no media → a single text part, no blobs.
func TestGeminiParts_textOnly(t *testing.T) {
	t.Parallel()

	parts := geminiParts("hi", nil)
	if len(parts) != 1 || parts[0].Text != "hi" || parts[0].InlineData != nil {
		t.Fatalf("text-only should be one text part with no blob, got %+v", parts)
	}
}

// Claude: media maps to a base64 image block in the user message.
func TestClaudeUserMessage_carriesBase64ImageBlock(t *testing.T) {
	t.Parallel()

	data := []byte("\x89PNG\r\n\x1a\npng-bytes")

	raw, err := json.Marshal(claudeUserMessage("describe", []resolvedMedia{rm("image/png", data)}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(raw)
	for _, want := range []string{"describe", "base64", "image/png", base64.StdEncoding.EncodeToString(data)} {
		if !strings.Contains(s, want) {
			t.Errorf("claude message missing %q\n%s", want, s)
		}
	}
}

func TestClaudeUserMessage_textOnly(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(claudeUserMessage("just text", nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if s := string(raw); strings.Contains(s, "base64") || strings.Contains(s, "image") {
		t.Fatalf("text-only claude message should carry no image block: %s", s)
	}
}

// OpenAI: media maps to an image_url content part with a base64 data URI.
func TestOpenAIUserMessage_carriesImageURL(t *testing.T) {
	t.Parallel()

	data := []byte("\x89PNG\r\n\x1a\npng-bytes")

	raw, err := json.Marshal(openaiUserMessage("describe", []resolvedMedia{rm("image/png", data)}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(raw)
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)

	for _, want := range []string{"describe", "image_url", dataURI} {
		if !strings.Contains(s, want) {
			t.Errorf("openai message missing %q\n%s", want, s)
		}
	}
}

func TestOpenAIUserMessage_textOnly(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(openaiUserMessage("just text", nil))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if s := string(raw); strings.Contains(s, "image_url") {
		t.Fatalf("text-only openai message should carry no image part: %s", s)
	}
}

// Claude: a PDF maps to a base64 document block (not an image block).
func TestClaudeUserMessage_carriesPDFDocumentBlock(t *testing.T) {
	t.Parallel()

	data := []byte("%PDF-1.7 pdf-bytes")

	raw, err := json.Marshal(claudeUserMessage("summarise", []resolvedMedia{rm("application/pdf", data)}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(raw)
	for _, want := range []string{"summarise", "document", "application/pdf", base64.StdEncoding.EncodeToString(data)} {
		if !strings.Contains(s, want) {
			t.Errorf("claude PDF message missing %q\n%s", want, s)
		}
	}

	if strings.Contains(s, "\"image\"") {
		t.Errorf("a PDF should not map to an image block: %s", s)
	}
}

// OpenAI: a PDF maps to a file content part with a base64 data URI.
func TestOpenAIUserMessage_carriesPDFFilePart(t *testing.T) {
	t.Parallel()

	data := []byte("%PDF-1.7 pdf-bytes")

	raw, err := json.Marshal(openaiUserMessage("summarise", []resolvedMedia{rm("application/pdf", data)}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(raw)
	dataURI := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(data)

	for _, want := range []string{"summarise", "file", dataURI} {
		if !strings.Contains(s, want) {
			t.Errorf("openai PDF message missing %q\n%s", want, s)
		}
	}
}
