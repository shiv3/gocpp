package routernats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/shiv3/gocpp/core/storage"
)

const (
	defaultSubjectPrefix = "gocpp.route"
	defaultFlushTimeout  = 5 * time.Second
)

var (
	errNilConnection = errors.New("router-nats: nil NATS connection")
	errNilRegistry   = errors.New("router-nats: nil connection registry")
	errNilHandler    = errors.New("router-nats: nil remote handler")
)

// Option configures a NATS-backed MessageRouter.
type Option func(*config)

type config struct {
	subjectPrefix string
	flushTimeout  time.Duration
}

func defaultConfig() config {
	return config{
		subjectPrefix: defaultSubjectPrefix,
		flushTimeout:  defaultFlushTimeout,
	}
}

// WithSubjectPrefix changes the subject prefix used for per-instance routing.
// The default subject for instance "csms-a" is "gocpp.route.csms-a".
func WithSubjectPrefix(prefix string) Option {
	return func(c *config) {
		c.subjectPrefix = strings.TrimSuffix(prefix, ".")
	}
}

// New returns a storage.MessageRouter backed by NATS request/reply.
func New(nc *nats.Conn, instanceID string, reg storage.ConnectionRegistry, opts ...Option) storage.MessageRouter {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return newRouter(natsTransport{nc: nc, flushTimeout: cfg.flushTimeout}, instanceID, reg, cfg)
}

func newRouter(transport transport, instanceID string, reg storage.ConnectionRegistry, cfg config) storage.MessageRouter {
	return &router{
		transport:     transport,
		instanceID:    instanceID,
		reg:           reg,
		subjectPrefix: cfg.subjectPrefix,
	}
}

type router struct {
	transport     transport
	instanceID    string
	reg           storage.ConnectionRegistry
	subjectPrefix string
}

var _ storage.MessageRouter = (*router)(nil)

func (r *router) CallLocal(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrNotLocal
}

func (r *router) CallRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
	if r.reg == nil {
		return nil, errNilRegistry
	}
	if len(req) > 0 && !json.Valid(req) {
		return nil, fmt.Errorf("router-nats: invalid request payload JSON")
	}

	target, ok, err := r.reg.LookupGlobal(ctx, cpID)
	if err != nil {
		return nil, fmt.Errorf("router-nats: lookup global connection for %q: %w", cpID, err)
	}
	if !ok || target == "" {
		return nil, storage.ErrNotLocal
	}

	data, err := json.Marshal(requestEnvelope{
		CPID:    cpID,
		Action:  action,
		Payload: json.RawMessage(req),
	})
	if err != nil {
		return nil, fmt.Errorf("router-nats: encode request: %w", err)
	}

	reply, err := r.transport.request(ctx, r.subject(target), data)
	if err != nil {
		return nil, requestError(target, err)
	}

	var env responseEnvelope
	if err := json.Unmarshal(reply, &env); err != nil {
		return nil, fmt.Errorf("router-nats: decode response from instance %q: %w", target, err)
	}
	if env.Error != "" {
		return nil, fmt.Errorf("router-nats: remote handler on instance %q: %w", target, remoteError(env.Error))
	}
	return []byte(env.Payload), nil
}

func (r *router) ServeRemote(ctx context.Context, handler storage.RemoteHandler) error {
	if handler == nil {
		return errNilHandler
	}

	sub, err := r.transport.subscribe(r.subject(r.instanceID), func(msg inboundMessage) {
		r.serveMessage(ctx, handler, msg)
	})
	if err != nil {
		return fmt.Errorf("router-nats: subscribe for instance %q: %w", r.instanceID, err)
	}
	if err := r.transport.flush(ctx); err != nil {
		return errors.Join(fmt.Errorf("router-nats: flush subscription for instance %q: %w", r.instanceID, err), sub.unsubscribe())
	}

	<-ctx.Done()
	if err := sub.drain(); err != nil {
		return errors.Join(ctx.Err(), fmt.Errorf("router-nats: drain subscription for instance %q: %w", r.instanceID, err), sub.unsubscribe())
	}
	return ctx.Err()
}

func (r *router) serveMessage(ctx context.Context, handler storage.RemoteHandler, msg inboundMessage) {
	var req requestEnvelope
	if err := json.Unmarshal(msg.data, &req); err != nil {
		msg.respond(responseEnvelope{Error: fmt.Sprintf("router-nats: decode request: %v", err)})
		return
	}

	payload, err := handler(ctx, req.CPID, req.Action, []byte(req.Payload))
	if err != nil {
		msg.respond(responseEnvelope{Error: err.Error()})
		return
	}
	msg.respond(responseEnvelope{Payload: json.RawMessage(payload)})
}

func (r *router) subject(instanceID string) string {
	if r.subjectPrefix == "" {
		return instanceID
	}
	return r.subjectPrefix + "." + instanceID
}

type requestEnvelope struct {
	CPID    string          `json:"cpID"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type responseEnvelope struct {
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func requestError(instanceID string, err error) error {
	if errors.Is(err, nats.ErrNoResponders) {
		return fmt.Errorf("%w: no NATS responder for instance %q", storage.ErrNotLocal, instanceID)
	}
	if errors.Is(err, nats.ErrTimeout) {
		return fmt.Errorf("router-nats: request to instance %q timed out: %w", instanceID, err)
	}
	return fmt.Errorf("router-nats: request to instance %q: %w", instanceID, err)
}

func remoteError(message string) error {
	switch message {
	case storage.ErrNotLocal.Error():
		return storage.ErrNotLocal
	case storage.ErrRouterNotImplemented.Error():
		return storage.ErrRouterNotImplemented
	default:
		return errors.New(message)
	}
}

type transport interface {
	request(ctx context.Context, subject string, data []byte) ([]byte, error)
	subscribe(subject string, handler func(inboundMessage)) (subscription, error)
	flush(ctx context.Context) error
}

type subscription interface {
	drain() error
	unsubscribe() error
}

type inboundMessage struct {
	data    []byte
	respond func(responseEnvelope)
}

type natsTransport struct {
	nc           *nats.Conn
	flushTimeout time.Duration
}

func (t natsTransport) request(ctx context.Context, subject string, data []byte) ([]byte, error) {
	if t.nc == nil {
		return nil, errNilConnection
	}

	msg, err := t.nc.RequestWithContext(ctx, subject, data)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

func (t natsTransport) subscribe(subject string, handler func(inboundMessage)) (subscription, error) {
	if t.nc == nil {
		return nil, errNilConnection
	}

	sub, err := t.nc.Subscribe(subject, func(msg *nats.Msg) {
		handler(inboundMessage{
			data: msg.Data,
			respond: func(env responseEnvelope) {
				data, err := json.Marshal(env)
				if err != nil {
					data = []byte(`{"error":"router-nats: encode response"}`)
				}
				_ = msg.Respond(data)
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return natsSubscription{sub: sub}, nil
}

func (t natsTransport) flush(ctx context.Context) error {
	if t.nc == nil {
		return errNilConnection
	}

	if _, ok := ctx.Deadline(); ok {
		return t.nc.FlushWithContext(ctx)
	}
	flushTimeout := t.flushTimeout
	if flushTimeout <= 0 {
		flushTimeout = defaultFlushTimeout
	}
	flushCtx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	return t.nc.FlushWithContext(flushCtx)
}

type natsSubscription struct {
	sub *nats.Subscription
}

func (s natsSubscription) drain() error {
	if s.sub == nil {
		return nil
	}
	return s.sub.Drain()
}

func (s natsSubscription) unsubscribe() error {
	if s.sub == nil {
		return nil
	}
	return s.sub.Unsubscribe()
}
