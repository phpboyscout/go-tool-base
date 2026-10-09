package root

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOTelAuthFrom(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "compiled", otelAuthFrom("", "compiled"), "no raw token keeps the compiled-in value")
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(otelInstanceID+":token")), otelAuthFrom("token", "compiled"),
		"a raw token is encoded with the instance ID")
}
