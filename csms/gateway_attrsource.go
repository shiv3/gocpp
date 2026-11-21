package csms

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/shiv3/gocpp/gateway"
)

const defaultAttrSourceTimeout = 200 * time.Millisecond

// lookupExt asks the attribute source for cpID and returns the attributes
// prefixed "ext.". ok is false when the lookup failed.
func (e *edge) lookupExt(ctx context.Context, cpID string) (map[string]string, bool) {
	o := e.s.cfg.attrSourceOpts
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultAttrSourceTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	got, err := e.s.cfg.attrSource.Attrs(ctx, cpID)
	result := "ok"
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		result = "timeout"
	case err != nil:
		result = "error"
	}
	if o.OnResult != nil {
		o.OnResult(result)
	}
	if result != "ok" {
		return nil, false
	}
	out := make(map[string]string, len(got))
	for k, v := range got {
		if k != "" && v != "" {
			out[gateway.ExtAttrPrefix+strings.TrimPrefix(k, gateway.ExtAttrPrefix)] = v
		}
	}
	return out, true
}

// withExtAttrs adds the external attributes to the backend's, never replacing a
// key the backend set, and never beyond the attribute size cap (the external
// ones are dropped then, not the backend's).
func (e *edge) withExtAttrs(ctx context.Context, cpID string, attrs map[string]string) map[string]string {
	if e.s.cfg.attrSource == nil {
		return attrs
	}
	ext, ok := e.lookupExt(ctx, cpID)
	if !ok || len(ext) == 0 {
		return attrs
	}
	out := make(map[string]string, len(attrs)+len(ext))
	size := 0
	for k, v := range attrs {
		out[k] = v
		size += len(k) + len(v)
	}
	for k, v := range ext {
		if _, taken := out[k]; taken {
			continue
		}
		if size += len(k) + len(v); size > gateway.MaxAttrsSize {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// refreshExt re-reads the external attributes of every connected session until
// the server stops.
func (e *edge) refreshExt(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-e.s.ctx.Done():
			return
		case <-t.C:
		}
		e.s.mu.RLock()
		conns := make([]*Conn, 0, len(e.s.conns))
		for _, c := range e.s.conns {
			if c.gw != nil {
				conns = append(conns, c)
			}
		}
		e.s.mu.RUnlock()
		for _, c := range conns {
			if e.s.ctx.Err() != nil {
				return
			}
			e.refreshOne(c)
		}
	}
}

func (e *edge) refreshOne(c *Conn) {
	ext, ok := e.lookupExt(e.s.ctx, c.gw.ref.CPID)
	if !ok {
		return
	}
	cur := c.gw.attrs()
	delta := map[string]string{}
	for k, v := range ext {
		if old, has := cur[k]; !has || old != v {
			delta[k] = v
		}
	}
	for k := range cur {
		if _, still := ext[k]; !still && strings.HasPrefix(k, gateway.ExtAttrPrefix) {
			delta[k] = "" // removed at the source
		}
	}
	if len(delta) == 0 {
		return
	}
	if _, err := c.gw.mergeAttrs(delta); err != nil {
		return // would exceed the cap: keep what the session has
	}
	// The operator-facing copy (the connection's attachment and, when clustered,
	// the registry record) follows best effort.
	_ = e.s.SetSessionAttrs(e.s.ctx, c.ID(), delta)
}
