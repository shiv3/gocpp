// Package controlauth protects the gocpp-csms serve-mode control API with an
// optional bearer token, for a hosted instance whose control listener is
// reachable from outside. /healthz and /readyz stay open for probes. Off by
// default.
package controlauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
)

// Extension adds --control-token.
type Extension struct {
	token string
}

// NewExtension returns the control API token extension.
func NewExtension() *Extension { return &Extension{} }

// Name implements server.Extension.
func (e *Extension) Name() string { return "control-token" }

// Flags implements server.Extension.
func (e *Extension) Flags(b *config.Binder) {
	b.String(&e.token, "control-token", "", `require "Authorization: Bearer <token>" on the control API except /healthz and /readyz (empty = no authentication); prefer the environment variable over the flag`)
}

// Setup implements server.Extension.
func (e *Extension) Setup(h *server.Hooks) error {
	if e.token == "" {
		return nil
	}
	h.WrapControl(Middleware(e.token))
	return nil
}

// Middleware returns a handler wrapper that requires the bearer token. The
// comparison is constant-time (over SHA-256 digests, so the token length is
// not revealed either).
func Middleware(token string) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/healthz", "/readyz":
				next.ServeHTTP(w, r)
				return
			}
			got, ok := bearer(r.Header.Get("Authorization"))
			sum := sha256.Sum256([]byte(got))
			if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="gocpp-csms"`)
				httpapi.WriteError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearer(h string) (string, bool) {
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
