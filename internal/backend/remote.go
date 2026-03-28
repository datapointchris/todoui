package backend

import "net/http"

// RemoteBackend connects to a todoui API server over HTTP.
type RemoteBackend struct {
	client *http.Client
	apiURL string
}

// NewRemoteBackend creates a backend that communicates with a remote API server.
func NewRemoteBackend(apiURL string) *RemoteBackend {
	return &RemoteBackend{
		client: &http.Client{},
		apiURL: apiURL,
	}
}
