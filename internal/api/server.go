package api

import "github.com/datapointchris/todoui/internal/backend"

// Server holds the dependencies for the HTTP API.
type Server struct {
	backend backend.Backend
}

// NewServer creates an API server backed by the given Backend implementation.
func NewServer(b backend.Backend) *Server {
	return &Server{backend: b}
}
