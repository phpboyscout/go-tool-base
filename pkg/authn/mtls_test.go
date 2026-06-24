package authn_test

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
)

func chain(leaf *x509.Certificate) [][]*x509.Certificate {
	return [][]*x509.Certificate{{leaf}}
}

func TestMTLSVerifier_DefaultSubjectPrecedence(t *testing.T) {
	t.Parallel()

	v := authn.NewMTLSVerifier()

	// Common Name wins.
	id, err := v.VerifyCert(context.Background(), chain(&x509.Certificate{
		Subject:  pkix.Name{CommonName: "client-a"},
		DNSNames: []string{"a.example"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "client-a", id.Subject)
	assert.Equal(t, "mtls", id.Method)

	// No CN -> first DNS SAN.
	id, err = v.VerifyCert(context.Background(), chain(&x509.Certificate{DNSNames: []string{"svc.internal"}}))
	require.NoError(t, err)
	assert.Equal(t, "svc.internal", id.Subject)

	// No CN, no DNS -> first URI SAN.
	id, err = v.VerifyCert(context.Background(), chain(&x509.Certificate{
		URIs: []*url.URL{{Scheme: "spiffe", Host: "trust", Path: "/ns/default/sa/api"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, "spiffe://trust/ns/default/sa/api", id.Subject)
}

func TestMTLSVerifier_Rejections(t *testing.T) {
	t.Parallel()

	v := authn.NewMTLSVerifier()

	_, err := v.VerifyCert(context.Background(), nil)
	require.ErrorIs(t, err, authn.ErrUnauthenticated, "no chains")

	_, err = v.VerifyCert(context.Background(), [][]*x509.Certificate{{}})
	require.ErrorIs(t, err, authn.ErrUnauthenticated, "empty leaf list")

	_, err = v.VerifyCert(context.Background(), chain(&x509.Certificate{}))
	require.ErrorIs(t, err, authn.ErrUnauthenticated, "no usable subject")
}

func TestMTLSVerifier_WithCertSubject(t *testing.T) {
	t.Parallel()

	v := authn.NewMTLSVerifier(authn.WithCertSubject(func(c *x509.Certificate) string {
		return "org:" + c.Subject.Organization[0]
	}))

	id, err := v.VerifyCert(context.Background(), chain(&x509.Certificate{
		Subject: pkix.Name{CommonName: "ignored", Organization: []string{"acme"}},
	}))
	require.NoError(t, err)
	assert.Equal(t, "org:acme", id.Subject)
}
