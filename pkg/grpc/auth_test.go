package grpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// authStream is a minimal grpc.ServerStream carrying a context.
type authStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authStream) Context() context.Context { return s.ctx }

func keyVerifier(t *testing.T, key, subject string) authn.Verifier {
	t.Helper()

	v, err := authn.NewAPIKeyVerifier(authn.KeyEntry{Key: key, Subject: subject})
	require.NoError(t, err)

	return v
}

func bearerCtx(token string) context.Context {
	return metadata.NewIncomingContext(context.Background(),
		metadata.New(map[string]string{"authorization": "Bearer " + token}))
}

func mdCtx(pairs map[string]string) context.Context {
	return metadata.NewIncomingContext(context.Background(), metadata.New(pairs))
}

func mtlsCtx(cn string) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{
			VerifiedChains: [][]*x509.Certificate{{{Subject: pkix.Name{CommonName: cn}}}},
		}},
	})
}

func unaryInfo(method string) *grpc.UnaryServerInfo { return &grpc.UnaryServerInfo{FullMethod: method} }

// captureUnary records the identity the handler sees and returns "ok".
func captureUnary(seen **authn.Identity) grpc.UnaryHandler {
	return func(ctx context.Context, _ any) (any, error) {
		id, _ := IdentityFromContext(ctx)
		*seen = id

		return "ok", nil
	}
}

func TestAuthInterceptor_ConstructionError(t *testing.T) {
	t.Parallel()

	_, err := AuthInterceptor()
	require.Error(t, err, "no verifier must fail closed")
}

func TestAuthInterceptor_UnaryBearer(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCBearerVerifier(keyVerifier(t, "tok", "alice")))
	require.NoError(t, err)

	var seen *authn.Identity
	resp, err := ic.Unary(bearerCtx("tok"), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	require.NotNil(t, seen)
	assert.Equal(t, "alice", seen.Subject)

	// Bad token -> Unauthenticated, handler not run.
	seen = nil
	_, err = ic.Unary(bearerCtx("wrong"), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Nil(t, seen)
}

func TestAuthInterceptor_UnaryAPIKeyMetadata(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCAPIKeyMetadata("x-api-key", keyVerifier(t, "secret", "ci")))
	require.NoError(t, err)

	var seen *authn.Identity
	_, err = ic.Unary(mdCtx(map[string]string{"x-api-key": "secret"}), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	require.NoError(t, err)
	assert.Equal(t, "ci", seen.Subject)

	// Missing credential -> Unauthenticated.
	_, err = ic.Unary(context.Background(), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_AmbiguousRejected(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(
		WithGRPCBearerVerifier(keyVerifier(t, "tok", "a")),
		WithGRPCAPIKeyMetadata("x-api-key", keyVerifier(t, "key", "b")),
	)
	require.NoError(t, err)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.New(map[string]string{
		"authorization": "Bearer tok",
		"x-api-key":     "key",
	}))

	var seen *authn.Identity
	_, err = ic.Unary(ctx, nil, unaryInfo("/svc/M"), captureUnary(&seen))
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "both schemes present must fail closed")
	assert.Nil(t, seen)
}

func TestAuthInterceptor_AuthorizeDenied(t *testing.T) {
	t.Parallel()

	deny := authn.AuthorizeFunc(func(_ context.Context, _ *authn.Identity) bool { return false })

	ic, err := AuthInterceptor(WithGRPCBearerVerifier(keyVerifier(t, "tok", "a")), WithGRPCAuthorize(deny))
	require.NoError(t, err)

	var seen *authn.Identity
	_, err = ic.Unary(bearerCtx("tok"), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Nil(t, seen)
}

func TestAuthInterceptor_AutoSkipsHealthAndReflection(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCBearerVerifier(keyVerifier(t, "tok", "a")))
	require.NoError(t, err)

	// No credentials at all, but the health/reflection methods are auto-skipped.
	for _, m := range []string{
		"/grpc.health.v1.Health/Check",
		"/grpc.health.v1.Health/Watch",
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	} {
		resp, err := ic.Unary(context.Background(), nil, unaryInfo(m),
			func(_ context.Context, _ any) (any, error) { return "ok", nil })
		require.NoErrorf(t, err, "method %s should be skipped", m)
		assert.Equal(t, "ok", resp)
	}
}

func TestAuthInterceptor_CustomSkipperIsAdditive(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(
		WithGRPCBearerVerifier(keyVerifier(t, "tok", "a")),
		WithGRPCMethodSkipper(func(m string) bool { return m == "/svc/Public" }),
	)
	require.NoError(t, err)

	// Custom-skipped method passes without a credential...
	_, err = ic.Unary(context.Background(), nil, unaryInfo("/svc/Public"),
		func(_ context.Context, _ any) (any, error) { return "ok", nil })
	require.NoError(t, err)

	// ...but a non-skipped method still requires auth.
	_, err = ic.Unary(context.Background(), nil, unaryInfo("/svc/Private"),
		func(_ context.Context, _ any) (any, error) { return "ok", nil })
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_MTLSFallback(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCMTLSVerifier(authn.NewMTLSVerifier()))
	require.NoError(t, err)

	var seen *authn.Identity
	_, err = ic.Unary(mtlsCtx("client-a"), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.Equal(t, "client-a", seen.Subject)
	assert.Equal(t, "mtls", seen.Method)

	// No peer cert -> Unauthenticated.
	_, err = ic.Unary(context.Background(), nil, unaryInfo("/svc/M"), captureUnary(&seen))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_StreamChecksAtOpen(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCBearerVerifier(keyVerifier(t, "tok", "alice")))
	require.NoError(t, err)

	info := &grpc.StreamServerInfo{FullMethod: "/svc/Stream"}

	// Success: handler runs and sees the identity via the stream context.
	var seen *authn.Identity
	err = ic.Stream(nil, &authStream{ctx: bearerCtx("tok")}, info, func(_ any, ss grpc.ServerStream) error {
		id, _ := IdentityFromContext(ss.Context())
		seen = id

		return nil
	})
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.Equal(t, "alice", seen.Subject)

	// Failure at open: handler never runs.
	ran := false
	err = ic.Stream(nil, &authStream{ctx: bearerCtx("wrong")}, info, func(_ any, _ grpc.ServerStream) error {
		ran = true

		return nil
	})
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, ran)
}

// fakeAuthInfo is a non-TLS credentials.AuthInfo for peer-cert negative tests.
type fakeAuthInfo struct{}

func (fakeAuthInfo) AuthType() string { return "fake" }

func TestAuthInterceptor_NonBearerAuthorizationIgnored(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCBearerVerifier(keyVerifier(t, "tok", "a")))
	require.NoError(t, err)

	// An "authorization" metadata that is not a Bearer scheme is treated as no
	// credential -> Unauthenticated.
	ctx := mdCtx(map[string]string{"authorization": "Basic dXNlcjpwYXNz"})
	_, err = ic.Unary(ctx, nil, unaryInfo("/svc/M"), func(_ context.Context, _ any) (any, error) { return "ok", nil })
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_MTLSNonTLSPeerRejected(t *testing.T) {
	t.Parallel()

	ic, err := AuthInterceptor(WithGRPCMTLSVerifier(authn.NewMTLSVerifier()))
	require.NoError(t, err)

	// Peer present but its AuthInfo is not TLS -> no usable client cert.
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: fakeAuthInfo{}})
	_, err = ic.Unary(ctx, nil, unaryInfo("/svc/M"), func(_ context.Context, _ any) (any, error) { return "ok", nil })
	assert.Equal(t, codes.Unauthenticated, status.Code(err))

	// Peer present with nil AuthInfo -> likewise rejected.
	ctx = peer.NewContext(context.Background(), &peer.Peer{})
	_, err = ic.Unary(ctx, nil, unaryInfo("/svc/M"), func(_ context.Context, _ any) (any, error) { return "ok", nil })
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInterceptor_RedactsCredentialInLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logger.NewCharm(&buf, logger.WithLevel(logger.DebugLevel))

	ic, err := AuthInterceptor(WithGRPCBearerVerifier(keyVerifier(t, "good", "a")), WithGRPCAuthLogger(logger.ToSlog(log)))
	require.NoError(t, err)

	_, _ = ic.Unary(bearerCtx("sk-abcdefghijklmnopqrstuvwxyz0123456789ABCD"), nil, unaryInfo("/svc/M"),
		func(_ context.Context, _ any) (any, error) { return "ok", nil })

	assert.NotContains(t, buf.String(), "sk-abcdefghijklmnopqrstuvwxyz0123456789ABCD",
		"the presented token must never appear in logs")
}
