package server

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/dispatch"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/recorder"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/txn"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/csms"
)

// Extension is an optional serve-mode feature. Extensions are compiled in
// from cmd/gocpp-csms/ext_<name>.go (an init function appending to
// serveExtensions) and live in their own internal package, so independent
// features never edit the same file.
//
// Lifecycle: Flags is called before the command line is parsed, Setup after
// it (before the csms.Server is built), the functions registered with
// Hooks.OnStart once both listeners are bound.
type Extension interface {
	// Name identifies the extension in logs and errors.
	Name() string
	// Flags declares the extension's flags; their environment defaults
	// follow the GOCPP_CSMS_<FLAG> rule of the Binder.
	Flags(b *config.Binder)
	// Setup registers the extension's hooks. Returning an error stops the
	// process with exit status 1.
	Setup(h *Hooks) error
}

// Hooks is what an extension may plug into during Setup.
type Hooks struct {
	// Config is the parsed serve-mode configuration (read-only).
	Config *config.Config
	// Logger is the process logger.
	Logger *slog.Logger
	// Inbound answers charge point CALLs: Override an action's answer,
	// Observe every handled CALL.
	Inbound *inbound.Handler
	// Txns is the transaction store.
	Txns *txn.Store
	// Recorder is the stdout NDJSON recorder; nil unless --record.
	Recorder *recorder.Recorder
	// Metrics registers extra Prometheus collectors served on /metrics.
	Metrics prometheus.Registerer

	options        []csms.Option
	frameObservers []observability.FrameObserver
	onConnect      []func(*csms.Conn)
	onDisconnect   []func(*csms.Conn, error)
	routes         []route
	authenticator  auth.Authenticator
	cpidExtractor  csms.CPIDExtractor
	onStart        []func(ctx context.Context, rt *Runtime) error
}

type route struct {
	pattern string
	handler http.Handler
}

// CSMSOptions adds csms.Server options (applied after the built-in ones, so
// they win where options conflict). For a frame observer, connect or
// disconnect callback, authenticator or charge point id extractor use the
// dedicated hook instead: those options take a single function and would
// replace the built-in one.
func (h *Hooks) CSMSOptions(opts ...csms.Option) { h.options = append(h.options, opts...) }

// FrameObserver adds a raw-frame observer (every frame in both directions;
// it runs inline on the connection's reader/writer, so keep it fast).
func (h *Hooks) FrameObserver(fn observability.FrameObserver) {
	h.frameObservers = append(h.frameObservers, fn)
}

// OnConnect adds a callback run when a charge point connection is up.
func (h *Hooks) OnConnect(fn func(*csms.Conn)) { h.onConnect = append(h.onConnect, fn) }

// OnDisconnect adds a callback run when a charge point connection ended.
func (h *Hooks) OnDisconnect(fn func(*csms.Conn, error)) {
	h.onDisconnect = append(h.onDisconnect, fn)
}

// Handle adds a control API route (http.ServeMux pattern syntax, e.g.
// "GET /chargepoints/{id}"). Patterns must not collide with the built-in
// ones (/healthz, /readyz, /metrics, /command, /connections, /transactions).
func (h *Hooks) Handle(pattern string, handler http.Handler) {
	h.routes = append(h.routes, route{pattern, handler})
}

// SetAuthenticator replaces the charge point authenticator built from
// --basic-auth-file / --mtls-match-cpid (a dynamic credential registry, for
// example). Hooks.Authenticator returns the built-in one so a replacement can
// delegate to it. The last extension to call it wins.
func (h *Hooks) SetAuthenticator(a auth.Authenticator) { h.authenticator = a }

// Authenticator returns the authenticator currently configured.
func (h *Hooks) Authenticator() auth.Authenticator { return h.authenticator }

// SetCPIDExtractor replaces how the charge point id is taken from the
// upgrade request (default: the last path segment under --path-prefix).
func (h *Hooks) SetCPIDExtractor(fn csms.CPIDExtractor) { h.cpidExtractor = fn }

// OnStart adds a function run once the server is listening, with the
// runtime (csms.Server, command dispatcher). ctx is cancelled when the
// server shuts down; start long-running work as goroutines watching it.
func (h *Hooks) OnStart(fn func(ctx context.Context, rt *Runtime) error) {
	h.onStart = append(h.onStart, fn)
}

// Runtime is the running server, handed to OnStart functions.
type Runtime struct {
	Config     *config.Config
	Logger     *slog.Logger
	CSMS       *csms.Server
	Dispatcher *dispatch.Dispatcher
	Inbound    *inbound.Handler
	Txns       *txn.Store
}
