package chat_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestOpenAIMediaIntegration_seesTheImage sends an image through the extended
// ChatClient to OpenAI and confirms the reply reflects the image content (spec
// step 5), env-gated. Reuses redJPEG from the Gemini media integration test.
func TestOpenAIMediaIntegration_seesTheImage(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "chat")

	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("set OPENAI_API_KEY to run the OpenAI media integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := chat.New(ctx, &props.Props{Logger: logger.NewNoop()}, chat.Config{
		Provider: chat.ProviderOpenAI,
		Model:    "gpt-4o-mini",
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
