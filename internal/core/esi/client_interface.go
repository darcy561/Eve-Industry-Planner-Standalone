package esi

import (
	"context"
	"net/http"
)

// ClientInterface defines the interface for the ESI rate-limited client.
// This allows for easier testing and abstraction.
type ClientInterface interface {
	// Do performs a rate-limited HTTP request and returns the response body, response, and error.
	Do(ctx context.Context, method, path string, headers map[string]string) ([]byte, *http.Response, error)
	// DoRequest performs a rate-limited HTTP request and returns the response.
	// Useful when you need full control over reading the response body (e.g., for streaming).
	DoRequest(ctx context.Context, method, path string, headers map[string]string) (*http.Response, error)
}

