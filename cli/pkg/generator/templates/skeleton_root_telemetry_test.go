package templates

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSkeletonRoot_TelemetryAuthBase64Encoded is the guard for the gtb-core
// review finding: the generated init() must base64-encode the OTEL_API_KEY
// fallback (RFC 7617 Basic credentials must be base64), rather than assigning
// the raw value straight into the "Basic " header.
func TestSkeletonRoot_TelemetryAuthBase64Encoded(t *testing.T) {
	t.Parallel()

	f := SkeletonRoot(SkeletonRootData{
		Name:                  "mytool",
		TelemetryOTelEndpoint: "https://otlp.example.com/otlp",
	})

	var buf bytes.Buffer
	require.NoError(t, f.Render(&buf))
	out := buf.String()

	assert.Contains(t, out, "base64.StdEncoding.EncodeToString",
		"the OTEL_API_KEY fallback must be base64-encoded")
	assert.Contains(t, out, `"Basic " + otelAuth`,
		"the Authorization header must use the encoded otelAuth value")
	assert.NotContains(t, out, "otelAuth = v\n",
		"the raw env value must not be assigned to otelAuth unencoded")
}
