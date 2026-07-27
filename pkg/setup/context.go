package setup

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// propsCtxKey is the unexported context key under which a root command's Props
// is carried to its command handlers and middleware.
type propsCtxKey struct{}

// ContextWithProps returns a copy of ctx carrying p. The root pre-run stamps the
// command context with its own Props so the process-global middleware chain
// resolves the RIGHT Props at execution time rather than closing over whichever
// root was constructed first. A nil ctx is treated as context.Background.
func ContextWithProps(ctx context.Context, p *props.Props) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	return context.WithValue(ctx, propsCtxKey{}, p)
}

// PropsFromContext returns the Props stamped onto ctx by ContextWithProps, or
// nil when none is present (an auxiliary/fast-path command, or a context built
// outside the root pre-run). Middleware falls back to its registration-time
// Props in that case.
func PropsFromContext(ctx context.Context) *props.Props {
	if ctx == nil {
		return nil
	}

	p, _ := ctx.Value(propsCtxKey{}).(*props.Props)

	return p
}
