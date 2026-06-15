package routerredis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shiv3/gocpp/core/storage"
)

const envelopeVersion = 1

var (
	errInvalidConfig      = errors.New("routerredis: invalid configuration")
	errSubscriptionClosed = errors.New("routerredis: subscription closed")
)

type requestEnvelope struct {
	Version      int    `json:"version"`
	ID           string `json:"id"`
	CPID         string `json:"cp_id"`
	Action       string `json:"action"`
	Payload      []byte `json:"payload,omitempty"`
	ReplyChannel string `json:"reply_channel"`
}

type responseEnvelope struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Payload []byte `json:"payload,omitempty"`
	Error   string `json:"error,omitempty"`
}

type router struct {
	broker     broker
	instanceID string
	reg        storage.ConnectionRegistry
	config     config
}

var _ storage.MessageRouter = (*router)(nil)

// New returns a Redis Pub/Sub backed storage.MessageRouter for instanceID.
func New(rdb *redis.Client, instanceID string, reg storage.ConnectionRegistry, opts ...Option) storage.MessageRouter {
	var b broker
	if rdb != nil {
		b = redisBroker{rdb: rdb}
	}
	return newRouter(b, instanceID, reg, opts...)
}

func newRouter(b broker, instanceID string, reg storage.ConnectionRegistry, opts ...Option) *router {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &router{
		broker:     b,
		instanceID: instanceID,
		reg:        reg,
		config:     cfg,
	}
}

func (r *router) CallLocal(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrNotLocal
}

func (r *router) CallRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}

	callCtx, cancel := r.callContext(ctx)
	defer cancel()

	targetInstance, ok, err := r.reg.LookupGlobal(callCtx, cpID)
	if err != nil {
		return nil, fmt.Errorf("routerredis: lookup global connection %q: %w", cpID, err)
	}
	if !ok {
		return nil, storage.ErrNotLocal
	}

	reqID := r.config.newID()
	if reqID == "" {
		return nil, fmt.Errorf("%w: empty request id", errInvalidConfig)
	}
	replyChannel := r.replyChannel(reqID)
	sub, err := r.broker.Subscribe(callCtx, replyChannel)
	if err != nil {
		return nil, fmt.Errorf("routerredis: subscribe reply channel: %w", err)
	}
	defer sub.Close()

	envelope := requestEnvelope{
		Version:      envelopeVersion,
		ID:           reqID,
		CPID:         cpID,
		Action:       action,
		Payload:      req,
		ReplyChannel: replyChannel,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("routerredis: encode request: %w", err)
	}
	if err := r.broker.Publish(callCtx, r.requestChannel(targetInstance), string(payload)); err != nil {
		return nil, fmt.Errorf("routerredis: publish request: %w", err)
	}

	for {
		select {
		case <-callCtx.Done():
			return nil, callCtx.Err()
		case msg, ok := <-sub.Channel():
			if !ok {
				return nil, errSubscriptionClosed
			}
			resp, ok := decodeResponse(msg.Payload, reqID)
			if !ok {
				continue
			}
			if resp.Error != "" {
				return nil, fmt.Errorf("routerredis: remote handler error: %s", resp.Error)
			}
			return resp.Payload, nil
		}
	}
}

func (r *router) ServeRemote(ctx context.Context, handler storage.RemoteHandler) error {
	if err := r.validate(); err != nil {
		return err
	}
	if handler == nil {
		return fmt.Errorf("%w: nil remote handler", errInvalidConfig)
	}

	sub, err := r.broker.Subscribe(ctx, r.requestChannel(r.instanceID))
	if err != nil {
		return fmt.Errorf("routerredis: subscribe request channel: %w", err)
	}
	defer sub.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-sub.Channel():
			if !ok {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return errSubscriptionClosed
				}
			}
			req, ok := decodeRequest(msg.Payload)
			if !ok {
				continue
			}
			go r.serveRequest(ctx, handler, req)
		}
	}
}

func (r *router) serveRequest(ctx context.Context, handler storage.RemoteHandler, req requestEnvelope) {
	resp, err := callHandler(ctx, handler, req.CPID, req.Action, req.Payload)
	envelope := responseEnvelope{
		Version: envelopeVersion,
		ID:      req.ID,
	}
	if err != nil {
		envelope.Error = err.Error()
	} else {
		envelope.Payload = resp
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return
	}
	_ = r.broker.Publish(ctx, req.ReplyChannel, string(payload))
}

func callHandler(ctx context.Context, handler storage.RemoteHandler, cpID, action string, req []byte) (resp []byte, err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("panic: %v", v)
		}
	}()
	return handler(ctx, cpID, action, req)
}

func decodeRequest(payload string) (requestEnvelope, bool) {
	var req requestEnvelope
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return requestEnvelope{}, false
	}
	if req.ID == "" || req.CPID == "" || req.Action == "" || req.ReplyChannel == "" {
		return requestEnvelope{}, false
	}
	return req, true
}

func decodeResponse(payload, reqID string) (responseEnvelope, bool) {
	var resp responseEnvelope
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		return responseEnvelope{}, false
	}
	if resp.ID != reqID {
		return responseEnvelope{}, false
	}
	return resp, true
}

func (r *router) validate() error {
	switch {
	case r == nil:
		return fmt.Errorf("%w: nil router", errInvalidConfig)
	case r.broker == nil:
		return fmt.Errorf("%w: nil Redis client", errInvalidConfig)
	case r.reg == nil:
		return fmt.Errorf("%w: nil connection registry", errInvalidConfig)
	case r.instanceID == "":
		return fmt.Errorf("%w: empty instance id", errInvalidConfig)
	case r.config.channelPrefix == "":
		return fmt.Errorf("%w: empty channel prefix", errInvalidConfig)
	case r.config.newID == nil:
		return fmt.Errorf("%w: nil request id generator", errInvalidConfig)
	default:
		return nil
	}
}

func (r *router) callContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.config.requestTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.config.requestTimeout)
}

func (r *router) requestChannel(instanceID string) string {
	return strings.Join([]string{r.config.channelPrefix, "requests", instanceID}, ":")
}

func (r *router) replyChannel(reqID string) string {
	return strings.Join([]string{r.config.channelPrefix, "replies", r.instanceID, reqID}, ":")
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
