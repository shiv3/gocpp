// Package attrsource provides gateway.AttrSource implementations: a file of
// per-charger labels and an HTTP lookup. Use them with csms.WithAttrSource when
// the labels a staged release selects on live in a system the backend does not
// own. Keys come back prefixed "ext." from the gateway.
//
// Experimental: this API may change before v1.0. Every exported identifier of
// this package is part of the split-gateway API; see "Stability" in
// docs/gateway.md.
package attrsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shiv3/gocpp/gateway"
	"gopkg.in/yaml.v3"
)

// File serves attributes from a JSON or YAML file shaped
//
//	CP_1: {site: berlin, label.canary: "1"}
//	CP_2: {site: paris}
//	"*":  {fleet: eu}        # optional: attributes of every charger (the charger's own win)
//
// The file is re-read when its modification time or size changes (checked at
// most once per second on use) and a bad file keeps the previous contents.
type File struct {
	Path string

	mu    sync.Mutex
	next  time.Time
	sig   string
	attrs map[string]map[string]string
}

var _ gateway.AttrSource = (*File)(nil)

// Attrs implements gateway.AttrSource.
func (f *File) Attrs(_ context.Context, cpID string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Now().After(f.next) {
		f.next = time.Now().Add(time.Second)
		if err := f.reloadLocked(); err != nil && f.attrs == nil {
			return nil, err
		}
	}
	out := map[string]string{}
	for k, v := range f.attrs["*"] {
		out[k] = v
	}
	for k, v := range f.attrs[cpID] {
		out[k] = v
	}
	return out, nil
}

func (f *File) reloadLocked() error {
	fi, err := os.Stat(f.Path)
	if err != nil {
		return err
	}
	sig := fmt.Sprint(fi.ModTime().UnixNano(), fi.Size())
	if sig == f.sig {
		return nil
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return err
	}
	var raw map[string]map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("attrsource: %s: %w", f.Path, err)
	}
	parsed := make(map[string]map[string]string, len(raw))
	for id, m := range raw {
		parsed[id] = make(map[string]string, len(m))
		for k, v := range m {
			parsed[id][k] = fmt.Sprint(v)
		}
	}
	f.attrs, f.sig = parsed, sig
	return nil
}

// HTTP looks a charger up with a GET. URL may contain "{cpid}" (path-escaped);
// without it the id is appended. The answer is a JSON object of string values; a
// 404 means "no attributes". Answers are cached for TTL (default 30 s, 0 =
// default, negative = no cache) so a reconnect storm does not hit the service.
type HTTP struct {
	URL    string
	Client *http.Client // default: a client with a 2 s timeout
	TTL    time.Duration
	// Header is added to every request (an Authorization token, say).
	Header http.Header

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	attrs map[string]string
	until time.Time
}

var _ gateway.AttrSource = (*HTTP)(nil)

const maxBody = 64 << 10

// Attrs implements gateway.AttrSource.
func (h *HTTP) Attrs(ctx context.Context, cpID string) (map[string]string, error) {
	ttl := h.TTL
	if ttl == 0 {
		ttl = 30 * time.Second
	}
	if ttl > 0 {
		h.mu.Lock()
		c, ok := h.cache[cpID]
		h.mu.Unlock()
		if ok && time.Now().Before(c.until) {
			return c.attrs, nil
		}
	}
	u := h.URL
	if strings.Contains(u, "{cpid}") {
		u = strings.ReplaceAll(u, "{cpid}", url.PathEscape(cpID))
	} else {
		u = strings.TrimRight(u, "/") + "/" + url.PathEscape(cpID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, vs := range h.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	cl := h.Client
	if cl == nil {
		cl = &http.Client{Timeout: 2 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var attrs map[string]string
	switch {
	case resp.StatusCode == http.StatusNotFound:
		attrs = map[string]string{}
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("attrsource: %s: %s", u, resp.Status)
	default:
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
		if err != nil {
			return nil, err
		}
		var raw map[string]any
		dec := json.NewDecoder(bytes.NewReader(body))
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("attrsource: %s: %w", u, err)
		}
		attrs = make(map[string]string, len(raw))
		for k, v := range raw {
			switch x := v.(type) {
			case string:
				attrs[k] = x
			case nil:
			case float64, bool:
				attrs[k] = fmt.Sprint(x)
			default:
				return nil, errors.New("attrsource: values must be strings, numbers or booleans")
			}
		}
	}
	if ttl > 0 {
		h.mu.Lock()
		if h.cache == nil {
			h.cache = make(map[string]cached)
		}
		if len(h.cache) > 100000 {
			h.cache = make(map[string]cached) // crude bound: start over
		}
		h.cache[cpID] = cached{attrs: attrs, until: time.Now().Add(ttl)}
		h.mu.Unlock()
	}
	return attrs, nil
}
