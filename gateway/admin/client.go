package admin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client calls the admin API of gateways.
type Client struct {
	HTTP  *http.Client // default: 10 s timeout
	Token string
	// TLS, when set, is used for HTTPS requests and makes a base without a
	// scheme (the gateway's lease label is host:port) mean https. Ignored when
	// HTTP is set.
	TLS *tls.Config
	// AllowPlaintextToken lets Token travel over http:// to a non-loopback host.
	// By default the client refuses: the bearer token would be readable (and the
	// address comes from a lease label anyone with Redis access can set).
	AllowPlaintextToken bool
}

// loopbackHost reports whether host (no port) is a loopback address or "localhost".
func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (c Client) do(ctx context.Context, method, base, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	if !strings.Contains(base, "://") {
		if c.TLS != nil {
			base = "https://" + base
		} else {
			base = "http://" + base
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, rd)
	if err != nil {
		return err
	}
	if c.Token != "" {
		if req.URL.Scheme != "https" && !c.AllowPlaintextToken && !loopbackHost(req.URL.Hostname()) {
			return fmt.Errorf("refusing to send the admin token over plain HTTP to %s: use TLS (--admin-ca / an https address)", req.URL.Host)
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
		if c.TLS != nil {
			hc.Transport = &http.Transport{TLSClientConfig: c.TLS}
		}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s: %s", method, base+path, resp.Status, strings.TrimSpace(string(data)))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Status reads the live status of the gateway at base (host:port or URL).
func (c Client) Status(ctx context.Context, base string) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodGet, base, "/admin/status", nil, &st)
	return st, err
}

// Shed starts a paced partial drain; it returns once the gateway has accepted it.
func (c Client) Shed(ctx context.Context, base string, req ShedRequest) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodPost, base, "/admin/shed", req, &st)
	return st, err
}

// SetSoftMax sets (n > 0) or removes (n == 0) the gateway's soft session cap.
func (c Client) SetSoftMax(ctx context.Context, base string, n int) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodPost, base, "/admin/soft-max", map[string]int{"n": n}, &st)
	return st, err
}

// WaitShed polls until the gateway's partial drain has finished.
func (c Client) WaitShed(ctx context.Context, base string, every time.Duration) (Status, error) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		st, err := c.Status(ctx, base)
		if err != nil {
			return st, err
		}
		if !st.PartialDrain && !st.Drain.Active {
			return st, nil
		}
		select {
		case <-ctx.Done():
			return st, ctx.Err()
		case <-t.C:
		}
	}
}
