package authn

import (
	"context"
	"crypto/x509"

	"github.com/cockroachdb/errors"
)

// CertVerifier authenticates a verified client-certificate chain from a
// completed mTLS handshake and returns the Identity, or an error. It is separate
// from Verifier because a client certificate is a transport property, not a
// bearer credential string.
//
// The cryptographic verification (chain validation against the configured client
// CA pool) is the TLS stack's job — the server must be configured for
// RequireAndVerifyClientCert with a ClientCAs pool. A CertVerifier receives the
// already-verified chains (as in tls.ConnectionState.VerifiedChains) and only
// derives identity from them.
type CertVerifier interface {
	VerifyCert(ctx context.Context, verifiedChains [][]*x509.Certificate) (*Identity, error)
}

// mtlsVerifier derives an Identity from the leaf of a verified client-cert chain.
type mtlsVerifier struct {
	subjectFn func(*x509.Certificate) string
}

// MTLSOption configures the mTLS verifier.
type MTLSOption func(*mtlsVerifier)

// WithCertSubject overrides how the Subject is derived from the leaf certificate.
// The default uses the Common Name, then the first DNS SAN, then the first URI
// SAN.
func WithCertSubject(fn func(*x509.Certificate) string) MTLSOption {
	return func(v *mtlsVerifier) {
		if fn != nil {
			v.subjectFn = fn
		}
	}
}

// NewMTLSVerifier returns a CertVerifier that derives the Subject from the leaf
// client certificate.
func NewMTLSVerifier(opts ...MTLSOption) CertVerifier {
	v := &mtlsVerifier{subjectFn: defaultCertSubject}
	for _, o := range opts {
		o(v)
	}

	return v
}

func defaultCertSubject(c *x509.Certificate) string {
	switch {
	case c.Subject.CommonName != "":
		return c.Subject.CommonName
	case len(c.DNSNames) > 0:
		return c.DNSNames[0]
	case len(c.URIs) > 0:
		return c.URIs[0].String()
	default:
		return ""
	}
}

func (v *mtlsVerifier) VerifyCert(_ context.Context, chains [][]*x509.Certificate) (*Identity, error) {
	// VerifiedChains is populated only when the TLS stack required and verified a
	// client certificate. An empty set means no verified client cert presented.
	if len(chains) == 0 || len(chains[0]) == 0 {
		return nil, errors.Wrap(ErrUnauthenticated, "no verified client certificate")
	}

	subject := v.subjectFn(chains[0][0])
	if subject == "" {
		return nil, errors.Wrap(ErrUnauthenticated, "client certificate has no usable subject")
	}

	return &Identity{Subject: subject, Method: "mtls"}, nil
}
