package chat_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

// MockServer represents a mock AI provider server.
type MockServer struct {
	*httptest.Server
	Handler func(w http.ResponseWriter, r *http.Request)
}

// NewMockServer creates a new MockServer.
func NewMockServer() *MockServer {
	m := &MockServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.Handler != nil {
			m.Handler(w, r)
		}
	}))
	return m
}

// RespondWithJSON sets the handler to respond with the given JSON.
func (m *MockServer) RespondWithJSON(statusCode int, data interface{}) {
	m.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// RespondWithStream sets the handler to respond with server-sent events.
func (m *MockServer) RespondWithStream(statusCode int, chunks []string) {
	m.Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(statusCode)
		flusher, _ := w.(http.Flusher)
		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
		}
	}
}
