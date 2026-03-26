package grpc

import "google.golang.org/grpc"

// Interceptor groups a paired unary and stream interceptor.
// Either field may be nil if the interceptor only applies to one RPC type.
type Interceptor struct {
	Unary  grpc.UnaryServerInterceptor
	Stream grpc.StreamServerInterceptor
}

// InterceptorChain composes zero or more gRPC interceptors into ordered
// slices suitable for grpc.ChainUnaryInterceptor and grpc.ChainStreamInterceptor.
type InterceptorChain struct {
	unary  []grpc.UnaryServerInterceptor
	stream []grpc.StreamServerInterceptor
}

// NewInterceptorChain creates a new interceptor chain.
// Each Interceptor argument provides a unary interceptor, a stream interceptor,
// or both. Nil entries in either field are silently skipped.
func NewInterceptorChain(interceptors ...Interceptor) InterceptorChain {
	var c InterceptorChain

	for _, i := range interceptors {
		if i.Unary != nil {
			c.unary = append(c.unary, i.Unary)
		}

		if i.Stream != nil {
			c.stream = append(c.stream, i.Stream)
		}
	}

	return c
}

// Append returns a new InterceptorChain with additional interceptors appended.
// The original chain is not modified.
func (c InterceptorChain) Append(interceptors ...Interceptor) InterceptorChain {
	result := InterceptorChain{
		unary:  make([]grpc.UnaryServerInterceptor, len(c.unary)),
		stream: make([]grpc.StreamServerInterceptor, len(c.stream)),
	}

	copy(result.unary, c.unary)
	copy(result.stream, c.stream)

	for _, i := range interceptors {
		if i.Unary != nil {
			result.unary = append(result.unary, i.Unary)
		}

		if i.Stream != nil {
			result.stream = append(result.stream, i.Stream)
		}
	}

	return result
}

// ServerOptions returns grpc.ServerOption values that install the chain.
// This is the primary integration point — pass the result to grpc.NewServer
// or to NewServer's variadic options.
//
//	chain := NewInterceptorChain(logging, recovery)
//	srv, _ := NewServer(cfg, chain.ServerOptions()...)
func (c InterceptorChain) ServerOptions() []grpc.ServerOption {
	var opts []grpc.ServerOption

	if len(c.unary) > 0 {
		opts = append(opts, grpc.ChainUnaryInterceptor(c.unary...))
	}

	if len(c.stream) > 0 {
		opts = append(opts, grpc.ChainStreamInterceptor(c.stream...))
	}

	return opts
}
