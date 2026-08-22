// Package httptestutil provides a recording HTTP test server for wire-level
// client tests. Handlers are registered with Go 1.22 method+path patterns;
// every request is recorded (method, path, query, body) so tests can assert
// on exactly what was sent, e.g. that a delete call carried deleteFiles=true.
package httptestutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Request is a recorded HTTP request.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// JSONBody unmarshals the recorded body into v.
func (r Request) JSONBody(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("failed to decode request body %q: %v", string(r.Body), err)
	}
}

// Server is a recording httptest server.
type Server struct {
	*httptest.Server
	t        *testing.T
	mux      *http.ServeMux
	mu       sync.Mutex
	requests []Request
}

// New starts a recording server; it is closed when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{t: t, mux: http.NewServeMux()}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.t.Errorf("failed to read request body: %v", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	s.mu.Lock()
	s.requests = append(s.requests, Request{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.Query(),
		Header: r.Header.Clone(),
		Body:   body,
	})
	s.mu.Unlock()

	s.mux.ServeHTTP(w, r)
}

// Handle registers a handler for a "METHOD /path" pattern.
func (s *Server) Handle(pattern string, handler http.HandlerFunc) {
	s.mux.HandleFunc(pattern, handler)
}

// JSON registers a handler that always responds with the JSON encoding of v.
func (s *Server) JSON(pattern string, v any) {
	s.Handle(pattern, func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(s.t, w, v)
	})
}

// OK registers a handler that responds 200 with an empty body.
func (s *Server) OK(pattern string) {
	s.Handle(pattern, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// Requests returns the recorded requests matching method and path
// (all requests when both are empty).
func (s *Server) Requests(method, path string) []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, 0, len(s.requests))
	for _, r := range s.requests {
		if (method == "" || r.Method == method) && (path == "" || r.Path == path) {
			out = append(out, r)
		}
	}
	return out
}

// WriteJSON writes v as a JSON response.
func WriteJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}
