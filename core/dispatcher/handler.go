package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/shiv3/gocpp/core/ocppj"
)

type handlerFunc func(ctx context.Context, c *Conn, payload []byte) ([]byte, error)

type handlerRegistry struct {
	mu sync.RWMutex
	hs map[string]handlerFunc
}

func newHandlerRegistry() *handlerRegistry {
	return &handlerRegistry{hs: make(map[string]handlerFunc)}
}

func (r *handlerRegistry) lookup(action string) (handlerFunc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.hs[action]
	return h, ok
}

func (r *handlerRegistry) register(action string, h handlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hs[action] = h
}

func (c *Conn) runHandler(frame ocppj.Frame) {
	defer c.sem.Release(1)

	h, ok := c.reg.lookup(frame.Action)
	if !ok {
		c.sendCallError(frame.MsgID, ocppj.NewCallError(
			ocppj.ErrorCodeNotImplemented, "action "+frame.Action+" not implemented", nil))
		return
	}
	resp, err := h(c.ctx, c, frame.Payload)
	if err != nil {
		c.sendCallError(frame.MsgID, mapHandlerError(err))
		return
	}
	c.sendCallResult(frame.MsgID, resp)
}

// mapHandlerError converts a handler error into a CallError per spec §6.5.
func mapHandlerError(err error) *ocppj.CallError {
	var ce *ocppj.CallError
	if errors.As(err, &ce) {
		return ce
	}
	switch {
	case errors.Is(err, ocppj.ErrUnknownAction):
		return ocppj.NewCallError(ocppj.ErrorCodeNotImplemented, err.Error(), nil)
	case errors.Is(err, ocppj.ErrInvalidDirection):
		return ocppj.NewCallError(ocppj.ErrorCodeMessageTypeNotSupported, err.Error(), nil)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return ocppj.NewCallError(ocppj.ErrorCodeInternalError, "request cancelled", nil)
	default:
		return ocppj.WrapCallError(ocppj.ErrorCodeInternalError, err, nil)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
