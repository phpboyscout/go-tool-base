package http

import "net/http"

// Middleware is the standard Go HTTP middleware signature.
type Middleware func(http.Handler) http.Handler

// Chain composes zero or more Middleware into a single Middleware.
// Middleware is applied left-to-right: the first middleware in the list is
// the outermost wrapper (first to see the request, last to see the response).
//
//	chain := NewChain(recovery, logging, auth)
//	handler := chain.Then(mux)
type Chain struct {
	middlewares []Middleware
}

// NewChain creates a new middleware chain from the given middleware functions.
// Nil entries are silently skipped.
func NewChain(middlewares ...Middleware) Chain {
	clean := make([]Middleware, 0, len(middlewares))
	for _, m := range middlewares {
		if m != nil {
			clean = append(clean, m)
		}
	}

	return Chain{middlewares: clean}
}

// Append returns a new Chain with additional middleware appended.
// The original chain is not modified. Nil entries are silently skipped.
func (c Chain) Append(middlewares ...Middleware) Chain {
	clean := make([]Middleware, len(c.middlewares), len(c.middlewares)+len(middlewares))
	copy(clean, c.middlewares)

	for _, m := range middlewares {
		if m != nil {
			clean = append(clean, m)
		}
	}

	return Chain{middlewares: clean}
}

// Extend returns a new Chain that applies c's middleware first, then other's.
func (c Chain) Extend(other Chain) Chain {
	return c.Append(other.middlewares...)
}

// Then applies the middleware chain to the given handler and returns
// the resulting http.Handler.
//
// If handler is nil, http.DefaultServeMux is used.
func (c Chain) Then(handler http.Handler) http.Handler {
	if handler == nil {
		handler = http.DefaultServeMux
	}

	for i := len(c.middlewares) - 1; i >= 0; i-- {
		handler = c.middlewares[i](handler)
	}

	return handler
}

// ThenFunc is a convenience for Then(http.HandlerFunc(fn)).
func (c Chain) ThenFunc(fn http.HandlerFunc) http.Handler {
	return c.Then(fn)
}
