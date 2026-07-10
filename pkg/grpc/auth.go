package grpc

import (
	"context"
	"crypto/x509"
	"log/slog"
	"strings"

	"github.com/cockroachdb/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"gitlab.com/phpboyscout/go-tool-base/pkg/authn"
	"gitlab.com/phpboyscout/go-tool-base/pkg/redact"
)

// GRPCAuthOption configures AuthInterceptor.
type GRPCAuthOption func(*grpcAuthConfig)

type grpcAuthConfig struct {
	bearer    authn.Verifier
	apiKeyMD  string
	apiKey    authn.Verifier
	mtls      authn.CertVerifier
	authorize authn.AuthorizeFunc
	log       *slog.Logger
	skip      func(fullMethod string) bool
}

// WithGRPCBearerVerifier extracts the token from the "authorization" metadata
// ("Bearer <token>") and verifies it.
func WithGRPCBearerVerifier(v authn.Verifier) GRPCAuthOption {
	return func(c *grpcAuthConfig) { c.bearer = v }
}

// WithGRPCAPIKeyMetadata extracts the credential from the named metadata key.
func WithGRPCAPIKeyMetadata(key string, v authn.Verifier) GRPCAuthOption {
	return func(c *grpcAuthConfig) { c.apiKeyMD = strings.ToLower(key); c.apiKey = v }
}

// WithGRPCMTLSVerifier authenticates the RPC from its verified client
// certificate when no metadata credential is presented.
func WithGRPCMTLSVerifier(v authn.CertVerifier) GRPCAuthOption {
	return func(c *grpcAuthConfig) { c.mtls = v }
}

// WithGRPCAuthorize installs an authorization predicate run after verification.
func WithGRPCAuthorize(fn authn.AuthorizeFunc) GRPCAuthOption {
	return func(c *grpcAuthConfig) { c.authorize = fn }
}

// WithGRPCAuthLogger sets the logger for redacted server-side failure logging.
func WithGRPCAuthLogger(l *slog.Logger) GRPCAuthOption {
	return func(c *grpcAuthConfig) { c.log = l }
}

// WithGRPCMethodSkipper skips auth for additional full method names. The standard
// health and reflection services are ALWAYS skipped so k8s gRPC probes keep
// working; this skipper adds to that set, it does not replace it.
func WithGRPCMethodSkipper(pred func(fullMethod string) bool) GRPCAuthOption {
	return func(c *grpcAuthConfig) { c.skip = pred }
}

// AuthInterceptor returns an Interceptor (unary + stream) that authenticates each
// RPC and stores the Identity in the RPC context. With no verifier it is a
// construction error (fail-closed).
//
// The standard health (/grpc.health.v1.Health/*) and reflection
// (/grpc.reflection.v1*) services are auto-skipped so probes keep working.
// Credential precedence mirrors the HTTP middleware: a bearer token and an
// API-key metadata value presented together are rejected as ambiguous; mTLS
// authenticates only when no metadata credential is presented. Failures yield a
// generic codes.Unauthenticated / codes.PermissionDenied; the cause is logged
// WARN with the credential redacted.
func AuthInterceptor(opts ...GRPCAuthOption) (Interceptor, error) {
	cfg := &grpcAuthConfig{}
	for _, o := range opts {
		o(cfg)
	}

	if cfg.bearer == nil && cfg.apiKey == nil && cfg.mtls == nil {
		return Interceptor{}, errors.New("grpc: AuthInterceptor requires at least one verifier (fail-closed)")
	}

	if cfg.log == nil {
		cfg.log = slog.New(slog.DiscardHandler)
	}

	return Interceptor{
		Unary: func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			newCtx, err := cfg.authorizeCtx(ctx, info.FullMethod)
			if err != nil {
				return nil, err
			}

			return handler(newCtx, req)
		},
		Stream: func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			newCtx, err := cfg.authorizeCtx(ss.Context(), info.FullMethod)
			if err != nil {
				return err
			}

			return handler(srv, &authServerStream{ServerStream: ss, ctx: newCtx})
		},
	}, nil
}

// authorizeCtx authenticates and authorizes an RPC, returning the enriched
// context or a generic gRPC status error.
func (cfg *grpcAuthConfig) authorizeCtx(ctx context.Context, fullMethod string) (context.Context, error) {
	if cfg.shouldSkip(fullMethod) {
		return ctx, nil
	}

	id, err := cfg.authenticate(ctx)
	if err != nil {
		cfg.log.Warn("authentication failed", "method", fullMethod, "error", redact.Error(err))

		return nil, status.Error(codes.Unauthenticated, "unauthenticated")
	}

	ctx = authn.ContextWithRequestMetadata(ctx, authn.RequestMetadata{Method: "grpc", Path: fullMethod})
	ctx = authn.ContextWithIdentity(ctx, id)

	if cfg.authorize != nil && !cfg.authorize(ctx, id) {
		cfg.log.Warn("authorization denied", "method", fullMethod, "subject", id.Subject)

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}

	return ctx, nil
}

func (cfg *grpcAuthConfig) shouldSkip(fullMethod string) bool {
	if isHealthOrReflection(fullMethod) {
		return true
	}

	return cfg.skip != nil && cfg.skip(fullMethod)
}

func isHealthOrReflection(m string) bool {
	return strings.HasPrefix(m, "/grpc.health.v1.Health/") ||
		strings.HasPrefix(m, "/grpc.reflection.v1.") ||
		strings.HasPrefix(m, "/grpc.reflection.v1alpha.")
}

func (cfg *grpcAuthConfig) authenticate(ctx context.Context) (*authn.Identity, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	bearerTok, bearerPresent := cfg.extractBearer(md)
	apiKeyVal, apiKeyPresent := cfg.extractAPIKey(md)

	switch {
	case bearerPresent && apiKeyPresent:
		return nil, errors.Wrap(authn.ErrUnauthenticated, "ambiguous credentials: bearer and api-key both presented")
	case bearerPresent:
		return cfg.bearer.Verify(ctx, bearerTok)
	case apiKeyPresent:
		return cfg.apiKey.Verify(ctx, apiKeyVal)
	default:
		return cfg.authenticateCert(ctx)
	}
}

// authenticateCert falls back to mTLS when configured and a verified client
// certificate is present, else reports no credential.
func (cfg *grpcAuthConfig) authenticateCert(ctx context.Context) (*authn.Identity, error) {
	if cfg.mtls != nil {
		if chains, ok := peerVerifiedChains(ctx); ok {
			return cfg.mtls.VerifyCert(ctx, chains)
		}
	}

	return nil, errors.Wrap(authn.ErrUnauthenticated, "no credential presented")
}

func (cfg *grpcAuthConfig) extractBearer(md metadata.MD) (string, bool) {
	if cfg.bearer == nil {
		return "", false
	}

	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", false
	}

	return parseBearer(vals[0])
}

func (cfg *grpcAuthConfig) extractAPIKey(md metadata.MD) (string, bool) {
	if cfg.apiKey == nil || cfg.apiKeyMD == "" {
		return "", false
	}

	vals := md.Get(cfg.apiKeyMD)
	if len(vals) == 0 || vals[0] == "" {
		return "", false
	}

	return vals[0], true
}

func parseBearer(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) >= len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):]), true
	}

	return "", false
}

func peerVerifiedChains(ctx context.Context) ([][]*x509.Certificate, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok || p.AuthInfo == nil {
		return nil, false
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 {
		return nil, false
	}

	return tlsInfo.State.VerifiedChains, true
}

// authServerStream overrides Context so the handler sees the authenticated ctx.
type authServerStream struct {
	grpc.ServerStream

	ctx context.Context
}

func (s *authServerStream) Context() context.Context { return s.ctx }

// IdentityFromContext returns the verified Identity set by AuthInterceptor. The
// same key is shared with the HTTP middleware.
func IdentityFromContext(ctx context.Context) (*authn.Identity, bool) {
	return authn.IdentityFromContext(ctx)
}
