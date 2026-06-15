package tenant

import (
	"net/http"
	"strings"
)

// Resolver extracts a tenant id from an HTTP request.
type Resolver func(r *http.Request) (tenantID string, ok bool)

// FromPathPrefix extracts the segment at index from the request URL path.
//
// Index is zero-based after trimming leading and trailing slashes. Empty
// segments, missing segments, negative indexes, and nil requests fail.
func FromPathPrefix(index int) Resolver {
	return func(r *http.Request) (string, bool) {
		if index < 0 || r == nil || r.URL == nil {
			return "", false
		}
		path := strings.Trim(r.URL.Path, "/")
		if path == "" {
			return "", false
		}
		segments := strings.Split(path, "/")
		if index >= len(segments) {
			return "", false
		}
		tenantID := strings.TrimSpace(segments[index])
		return tenantID, tenantID != ""
	}
}

// FromHeader extracts the tenant id from the named HTTP header.
func FromHeader(name string) Resolver {
	return func(r *http.Request) (string, bool) {
		if name == "" || r == nil {
			return "", false
		}
		tenantID := strings.TrimSpace(r.Header.Get(name))
		return tenantID, tenantID != ""
	}
}
