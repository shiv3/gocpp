package msglog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
)

// DefaultLimit is the number of entries GET /messages returns without limit.
const DefaultLimit = 100

// parseFilter reads cp, action, direction and since from the query string.
// since is a seq number (entries after it) or an RFC 3339 time (entries at
// or after it).
func parseFilter(r *http.Request) (Filter, error) {
	q := r.URL.Query()
	f := Filter{CPID: q.Get("cp"), Action: q.Get("action"), Direction: q.Get("direction")}
	switch f.Direction {
	case "", In, Out:
	default:
		return f, fmt.Errorf(`direction must be "in" or "out"`)
	}
	if s := q.Get("since"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
			f.SinceSeq, f.HasSinceSeq = n, true
		} else if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			f.SinceTime = t
		} else {
			return f, fmt.Errorf("since must be a seq number or an RFC 3339 time")
		}
	}
	return f, nil
}

// ListHandler serves GET /messages?cp=&action=&direction=&since=&limit=.
func ListHandler(r *Ring, maxLimit int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		f, err := parseFilter(req)
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		limit := DefaultLimit
		if s := req.URL.Query().Get("limit"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				httpapi.WriteError(w, http.StatusBadRequest, "limit must be a positive integer")
				return
			}
			limit = n
		}
		limit = min(limit, maxLimit)
		httpapi.WriteResult(w, r.Query(f, limit))
	})
}

// ClearHandler serves DELETE /messages: empties the ring buffer.
func ClearHandler(r *Ring) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		httpapi.WriteResult(w, map[string]int{"deleted": r.Clear()})
	})
}

// streamBuffer is the per-subscriber backlog before it is dropped.
const streamBuffer = 1024

// keepAlive is the interval of SSE comment lines on an idle stream.
var keepAlive = 15 * time.Second

// StreamHandler serves GET /messages/stream: server-sent events, one
// "message" event per matching entry with the seq as event id. A
// Last-Event-ID header (an automatic reconnect) acts as since. The stream
// ends when ctx is done (server shutdown).
func StreamHandler(ctx context.Context, r *Ring) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		f, err := parseFilter(req)
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if id := req.Header.Get("Last-Event-ID"); id != "" {
			if n, err := strconv.ParseInt(id, 10, 64); err == nil && n >= 0 {
				f.SinceSeq, f.HasSinceSeq, f.SinceTime = n, true, time.Time{}
			}
		}
		rc := http.NewResponseController(w)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		backlog, sub := r.Subscribe(f, streamBuffer)
		defer r.Unsubscribe(sub)
		write := func(e Entry) bool {
			b, err := json.Marshal(e)
			if err != nil {
				return true
			}
			_, err = fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", e.Seq, b)
			return err == nil
		}
		for _, e := range backlog {
			if !write(e) {
				return
			}
		}
		if _, err := fmt.Fprint(w, ": ready\n\n"); err != nil || rc.Flush() != nil {
			return
		}
		tick := time.NewTicker(keepAlive)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-req.Context().Done():
				return
			case <-tick.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil || rc.Flush() != nil {
					return
				}
			case e, ok := <-sub.C():
				if !ok {
					return
				}
				if !write(e) || rc.Flush() != nil {
					return
				}
			}
		}
	})
}
