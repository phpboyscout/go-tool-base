package chat_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
)

// redJPEG is a solid-red image — recognisable enough that a model which actually
// received it will mention the colour, proving the media reached Gemini.
func redJPEG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{R: 220, G: 20, B: 20, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))

	return buf.Bytes()
}

// TestGeminiMediaIntegration_seesTheImage sends an image through the extended
// ChatClient and confirms Gemini's reply reflects the image content — the
// end-to-end multimodal path (spec step 3), env-gated.
func TestGeminiMediaIntegration_seesTheImage(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")

	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("set GEMINI_API_KEY to run the Gemini media integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := newTestClient(ctx, chat.Config{
		Provider: chat.ProviderGemini,
		Model:    "gemini-3.5-flash",
		Token:    key,
	})
	require.NoError(t, err)

	reply, err := client.Chat(ctx,
		"What is the single dominant colour in this image? Answer with just the colour name.",
		chat.Media{MIMEType: "image/jpeg", Data: redJPEG(t)})
	require.NoError(t, err)
	require.NotEmpty(t, reply)

	if !strings.Contains(strings.ToLower(reply), "red") {
		t.Fatalf("model reply does not reflect the red image (media may not have reached it): %q", reply)
	}
}
