// Package gcpsourcetest isolates GCP source tests from the developer's
// environment.
package gcpsourcetest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

// keyBits and keyFileMode are the throwaway key's size and its file's mode.
const (
	keyBits     = 2048
	keyFileMode = 0o600
)

// Isolate points Application Default Credentials at a throwaway service
// account key, so credential detection succeeds without the network or the
// developer's gcloud login. It sets environment variables, so its caller
// cannot run in parallel.
func Isolate(t testing.TB) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		t.Fatal(err)
	}

	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	doc, err := json.Marshal(map[string]string{ //nolint:gosec // a key generated for this test alone
		"type":           "service_account",
		"project_id":     "gtb-test",
		"private_key_id": "test",
		"private_key":    string(pemKey),
		"client_email":   "test@gtb-test.iam.gserviceaccount.com",
		"client_id":      "1",
		"token_uri":      "https://oauth2.googleapis.com/token",
	})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "sa.json")

	if err := os.WriteFile(path, doc, keyFileMode); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)
	t.Setenv("CLOUDSDK_CONFIG", dir)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", path)
}
