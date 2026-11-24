package hosted

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/authn"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/csms"
)

// Extension is the hosted test CSMS. It does nothing unless --hosted is set.
type Extension struct {
	enabled      bool
	mode         string
	stateFile    string
	limit        int
	instancePath bool
	instanceID   string
	publicURL    string

	inst *Instance
}

// NewExtension returns the hosted test CSMS extension.
func NewExtension() *Extension { return &Extension{} }

// Name implements server.Extension.
func (e *Extension) Name() string { return "hosted" }

// Flags implements server.Extension.
func (e *Extension) Flags(b *config.Binder) {
	b.Bool(&e.enabled, "hosted", false, "run as a hosted test CSMS: open or private instance mode, connection limit, charge point registry API under /instance and /registrations")
	b.String(&e.mode, "hosted-mode", "", `instance mode at start: "open" (any charge point id) or "private" (registered charge points only); empty = the persisted mode, else open`)
	b.String(&e.stateFile, "hosted-state-file", "", "JSON file persisting the instance mode, instance id and registrations (written atomically); empty = in memory only")
	b.Int(&e.limit, "hosted-connection-limit", -1, "simultaneous charge point connections before new upgrades get HTTP 429 with Retry-After (-1 = no limit, 0 = refuse all)")
	b.Bool(&e.instancePath, "hosted-instance-path", false, "charge points dial <path-prefix><instanceId>/<cpId>; other paths are refused")
	b.String(&e.instanceID, "hosted-instance-id", "", "instance id of --hosted-instance-path (empty = the persisted one, else a generated random token)")
	b.String(&e.publicURL, "hosted-public-url", "", "WebSocket base URL announced in wsUrl/wsBaseUrl, path prefix included (e.g. wss://csms.example.com/ocpp/); empty = derived from --ocpp-addr and --path-prefix")
}

// Instance returns the running instance (nil unless --hosted).
func (e *Extension) Instance() *Instance { return e.inst }

// Setup implements server.Extension.
func (e *Extension) Setup(h *server.Hooks) error {
	if !e.enabled {
		if e.mode != "" || e.stateFile != "" || e.limit != -1 || e.instancePath || e.instanceID != "" || e.publicURL != "" {
			return errors.New("the --hosted-* flags need --hosted")
		}
		return nil
	}
	if e.limit < -1 {
		return errors.New("--hosted-connection-limit must be -1 (no limit) or more")
	}
	if e.instanceID != "" && !e.instancePath {
		return errors.New("--hosted-instance-id needs --hosted-instance-path")
	}
	reg, err := Open(e.stateFile)
	if err != nil {
		return fmt.Errorf("--hosted-state-file: %w", err)
	}
	if e.mode != "" {
		if err := reg.SetMode(e.mode); err != nil {
			return fmt.Errorf("--hosted-mode: %w", err)
		}
	}
	id := ""
	if e.instancePath {
		id = e.instanceID
		if id == "" {
			id = reg.InstanceID()
		}
		if id == "" {
			if id, err = NewInstanceID(); err != nil {
				return err
			}
		}
		if !validInstanceID(id) {
			return errors.New("--hosted-instance-id must be URL-safe ([A-Za-z0-9_-], at most 128 characters)")
		}
		if err := reg.SetInstanceID(id); err != nil {
			return err
		}
	}
	base, err := wsBase(h.Config, e.publicURL)
	if err != nil {
		return err
	}
	var static authn.Credentials
	if h.Config.BasicAuthFile != "" {
		if static, err = authn.LoadFile(h.Config.BasicAuthFile); err != nil {
			return fmt.Errorf("--basic-auth-file: %w", err)
		}
	}

	in := &Instance{
		reg:        reg,
		logger:     h.Logger,
		instanceID: id,
		wsBase:     base,
		prefix:     withSlash(h.Config.PathPrefix),
		conns:      map[*csms.Conn]struct{}{},
		ids:        map[string]int{},
	}
	in.limit.Store(int64(e.limit))
	in.auth = &authenticator{reg: reg, builtin: h.Authenticator(), static: static, mtls: h.Config.MTLSMatchCPID}
	e.inst = in

	h.SetAuthenticator(in.auth)
	if e.instancePath {
		h.SetCPIDExtractor(in.extractCPID)
	}
	h.WrapOCPP(in.admit)
	h.OnConnect(in.connected)
	h.OnDisconnect(func(c *csms.Conn, _ error) { in.disconnected(c) })
	in.routes(h)
	h.Logger.Info("hosted test CSMS", "mode", reg.Mode(), "connection_limit", e.limit, "registrations", len(reg.List()), "ws_base_url", in.baseURL(), "state_file", e.stateFile)
	return nil
}

// Instance is the state of a running hosted CSMS.
type Instance struct {
	reg        *Registry
	auth       *authenticator
	logger     *slog.Logger
	instanceID string
	wsBase     string
	prefix     string
	limit      atomic.Int64

	mu      sync.Mutex
	conns   map[*csms.Conn]struct{}
	ids     map[string]int // connected charge point ids
	pending int            // upgrades admitted and not yet connected or failed
}

// Registry returns the charge point registry.
func (in *Instance) Registry() *Registry { return in.reg }

// ActiveConnections returns the connected charge points.
func (in *Instance) ActiveConnections() int {
	in.mu.Lock()
	defer in.mu.Unlock()
	return len(in.conns)
}

func (in *Instance) isConnected(cpID string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.ids[cpID] > 0
}

func (in *Instance) connected(c *csms.Conn) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if _, ok := in.conns[c]; !ok {
		in.conns[c] = struct{}{}
		in.ids[c.ID()]++
	}
}

func (in *Instance) disconnected(c *csms.Conn) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if _, ok := in.conns[c]; ok {
		delete(in.conns, c)
		if in.ids[c.ID()]--; in.ids[c.ID()] <= 0 {
			delete(in.ids, c.ID())
		}
	}
}

// admit refuses an upgrade with 429 and Retry-After once the connection
// limit is reached. A charge point id that is already connected replaces its
// session and needs no slot. The upgrade counts as pending until the
// handler returns; the server publishes the connection (OnConnect) before
// that, so the count never dips while a session is being set up.
func (in *Instance) admit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := int(in.limit.Load())
		if limit < 0 {
			next.ServeHTTP(w, r)
			return
		}
		cpID, _ := in.cpidOf(r)
		in.mu.Lock()
		if in.ids[cpID] == 0 && len(in.conns)+in.pending >= limit {
			in.mu.Unlock()
			in.logger.Info("connection refused: connection limit reached", "cp_id", cpID, "limit", limit)
			w.Header().Set("Retry-After", "30")
			http.Error(w, "connection limit reached", http.StatusTooManyRequests)
			return
		}
		in.pending++
		in.mu.Unlock()
		defer func() {
			in.mu.Lock()
			in.pending--
			in.mu.Unlock()
		}()
		next.ServeHTTP(w, r)
	})
}

func (in *Instance) cpidOf(r *http.Request) (string, bool) {
	if in.instanceID != "" {
		return in.extractCPID(r)
	}
	rest, ok := strings.CutPrefix(r.URL.Path, in.prefix)
	if !ok || rest == "" || strings.HasSuffix(rest, "/") {
		return "", false
	}
	if i := strings.LastIndexByte(rest, '/'); i >= 0 {
		rest = rest[i+1:]
	}
	return rest, rest != ""
}

// extractCPID accepts exactly <prefix><instanceId>/<cpId>.
func (in *Instance) extractCPID(r *http.Request) (string, bool) {
	rest, ok := strings.CutPrefix(r.URL.Path, in.prefix)
	if !ok {
		return "", false
	}
	inst, cpID, ok := strings.Cut(rest, "/")
	if !ok || cpID == "" || strings.Contains(cpID, "/") {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(inst), []byte(in.instanceID)) != 1 {
		return "", false
	}
	return cpID, true
}

// baseURL is the WebSocket URL a charge point id is appended to.
func (in *Instance) baseURL() string {
	if in.instanceID != "" {
		return in.wsBase + url.PathEscape(in.instanceID) + "/"
	}
	return in.wsBase
}

// WSURL is the URL charge point cpID dials.
func (in *Instance) WSURL(cpID string) string { return in.baseURL() + url.PathEscape(cpID) }

// authenticator decides charge point upgrades by the instance mode:
//
//   - open: the built-in authenticator (--basic-auth-file and
//     --mtls-match-cpid when set; otherwise any charge point);
//   - private: a registered charge point must present its registry password
//     (HTTP Basic; the client certificate check of --mtls-match-cpid still
//     applies); an unregistered one is accepted only if --basic-auth-file
//     lists it, through the built-in authenticator; anything else is
//     refused with 401. A registration wins over a static entry of the same id.
type authenticator struct {
	reg     *Registry
	builtin auth.Authenticator
	static  authn.Credentials
	mtls    bool
}

func (a *authenticator) Authenticate(r *http.Request, cpID string) (auth.Identity, error) {
	if a.reg.Mode() == ModeOpen {
		return a.builtin.Authenticate(r, cpID)
	}
	if _, ok := a.reg.Lookup(cpID); ok {
		user, password, ok := r.BasicAuth()
		if !ok || !a.reg.Verify(cpID, password) {
			return auth.Identity{}, auth.ErrUnauthorized
		}
		if a.mtls {
			if _, err := authn.CertMatchesCPID().Authenticate(r, cpID); err != nil {
				return auth.Identity{}, err
			}
		}
		return auth.Identity{CPID: cpID, Method: auth.AuthMethodBasic, Credential: user}, nil
	}
	if _, ok := a.static[cpID]; ok {
		return a.builtin.Authenticate(r, cpID)
	}
	return auth.Identity{}, auth.ErrUnauthorized
}

func validInstanceID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func withSlash(s string) string {
	if strings.HasSuffix(s, "/") {
		return s
	}
	return s + "/"
}

// wsBase returns the announced WebSocket base URL (ending in /).
func wsBase(cfg *config.Config, public string) (string, error) {
	if public != "" {
		u, err := url.Parse(public)
		if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
			return "", fmt.Errorf("--hosted-public-url %q: expected ws://host/path/ or wss://host/path/", public)
		}
		return withSlash(public), nil
	}
	scheme := "ws"
	if cfg.TLSCert != "" {
		scheme = "wss"
	}
	host, port, err := net.SplitHostPort(cfg.OCPPAddr)
	if err != nil {
		return "", fmt.Errorf("--ocpp-addr %q: %w", cfg.OCPPAddr, err)
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "localhost"
	}
	return fmt.Sprintf("%s://%s%s", scheme, net.JoinHostPort(host, port), withSlash(cfg.PathPrefix)), nil
}
