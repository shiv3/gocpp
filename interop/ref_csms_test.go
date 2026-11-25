//go:build interop

package interop

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
)

// recorder keeps the payloads a gocpp endpoint received and answered, per action.
type recorder struct {
	mu   sync.Mutex
	in   map[string][][]byte
	outs map[string][][]byte
}

func newRecorder() *recorder {
	return &recorder{in: map[string][][]byte{}, outs: map[string][][]byte{}}
}

func (r *recorder) note(action string, req, resp []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.in[action] = append(r.in[action], req)
	if resp != nil {
		r.outs[action] = append(r.outs[action], resp)
	}
}

func (r *recorder) last(action string) (req, resp []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l := r.in[action]; len(l) > 0 {
		req = l[len(l)-1]
	}
	if l := r.outs[action]; len(l) > 0 {
		resp = l[len(l)-1]
	}
	return
}

func rawErr(e error) error {
	var ce *callErr
	if ok := asCallErr(e, &ce); ok {
		return &dispatcher.RawCallError{Err: &ocppj.CallError{Code: ocppj.ErrorCode(ce.code), Description: ce.desc}, Wire: ce.code}
	}
	return e
}

func asCallErr(e error, out **callErr) bool {
	ce, ok := e.(*callErr)
	if ok {
		*out = ce
	}
	return ok
}

// rawAnswer is a catch-all handler answering from the fixture as side, which
// records every request.
func rawAnswer(resp *responder, side string, rec *recorder) func(ctx context.Context, c *csms.Conn, action string, payload []byte) ([]byte, error) {
	return func(ctx context.Context, c *csms.Conn, action string, payload []byte) ([]byte, error) {
		out, err := resp.answer(side, action, payload)
		if err != nil {
			rec.note(action, append([]byte(nil), payload...), nil)
			return nil, rawErr(err)
		}
		rec.note(action, append([]byte(nil), payload...), out)
		return out, nil
	}
}

// gocppCSMS is a gocpp CSMS that answers from the fixture.
type gocppCSMS struct {
	srv  *csms.Server
	http *http.Server
	port int
	rec  *recorder
}

func startGocppCSMS(t *testing.T, v versionSpec, resp *responder, opts ...csms.Option) *gocppCSMS {
	t.Helper()
	rec := newRecorder()
	opts = append([]csms.Option{csms.WithSubProtocols(v.sub)}, opts...)
	srv := csms.NewServer(opts...)
	csms.OnAny(srv, rawAnswer(resp, "csms", rec))
	if err := registerTypedCSMS(srv, v.ocpp, resp, rec); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = hs.Serve(l) }()
	t.Cleanup(func() { _ = hs.Close(); srv.Close() })
	return &gocppCSMS{srv: srv, http: hs, port: l.Addr().(*net.TCPAddr).Port, rec: rec}
}

func (g *gocppCSMS) waitConn(t *testing.T, id string) *csms.Conn {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c, ok := g.srv.Get(id); ok {
			return c
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("station %s never connected to the gocpp CSMS", id)
	return nil
}

// TestInterop_GocppCSMS runs gocpp as the CSMS against each reference station
// (a peer implementation acting as a charge point), with CALLs from both ends.
func TestInterop_GocppCSMS(t *testing.T) {
	for _, p := range selectedPeers() {
		p := p
		t.Run(p.name, func(t *testing.T) {
			caps := loadCaps(t, p)
			for _, v := range selectedVersions() {
				v := v
				t.Run(v.ocpp, func(t *testing.T) {
					vc, ok := caps.Versions[v.ocpp]
					if !ok || !vc.Client.Supported {
						t.Skipf("%s has no station for OCPP %s", p.name, v.ocpp)
					}
					runGocppCSMS(t, p, v, vc)
				})
			}
		})
	}
}

func runGocppCSMS(t *testing.T, p peerSpec, v versionSpec, vc versionCaps) {
	fx := loadFixture(t, p, v)
	station := fx.Stations[0].ID
	resp := newResponder(fx, v.ocpp)
	srv := startGocppCSMS(t, v, resp)

	name := startContainer(t, p.imageRef(), nil, "client", "run", "--version", v.ocpp,
		"--url", fmt.Sprintf("ws://127.0.0.1:%d/ocpp", srv.port))
	conn := srv.waitConn(t, station)

	// station -> gocpp CSMS
	expect := newExpectation(fx, v.ocpp)
	for _, sn := range fx.sampleNames() {
		s := fx.Samples[sn]
		if s.From != "station" {
			continue
		}
		sn, s := sn, s
		t.Run("peer-to-gocpp/"+sn, func(t *testing.T) {
			if !vc.Client.sends(s.Action) {
				t.Skipf("%s does not send %s", p.name, s.Action)
			}
			want, err := expect.answer("csms", s.Action, s.Payload)
			if err != nil {
				t.Skipf("fixture has no answer: %v", err)
			}
			r := refCall(t, name, "", s.Action, s.Payload)
			if r.CallError != nil || !r.OK {
				failKnown(t, "peer got %s %v (gocpp saw %s)", r.Raw, r.CallError, lastReq(srv.rec, s.Action))
				return
			}
			if err := jsonSubset(want, r.Response); err != nil {
				failKnown(t, "answer differs from the fixture: %v (peer parsed %s)", err, r.Response)
				return
			}
			req, _ := srv.rec.last(s.Action)
			if err := jsonSubset(s.Payload, req); err != nil {
				failKnown(t, "gocpp received a different payload than sent: %v (got %s)", err, req)
			}
		})
	}

	// gocpp CSMS -> station
	for _, sn := range fx.sampleNames() {
		s := fx.Samples[sn]
		if s.From != "csms" {
			continue
		}
		sn, s := sn, s
		t.Run("gocpp-to-peer/"+sn, func(t *testing.T) {
			if !vc.Client.handles(s.Action) {
				t.Skipf("%s does not declare %s", p.name, s.Action)
			}
			want, err := newExpectation(fx, v.ocpp).answer("station", s.Action, s.Payload)
			if err != nil {
				t.Skipf("fixture has no answer: %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			out, err := callFromCSMS(ctx, conn, v.ocpp, s.Action, s.Payload)
			if err != nil {
				failKnown(t, "gocpp CALL failed: %v", err)
				return
			}
			if err := jsonSubset(want, out); err != nil {
				failKnown(t, "answer differs from the fixture: %v (got %s)", err, out)
			}
		})
	}

	t.Run("unknown-action/gocpp-to-peer", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := csms.CallRaw(ctx, conn, "NoSuchAction", []byte(`{}`))
		wantCallError(t, err)
	})
	t.Run("unanswered-action/peer-to-gocpp", func(t *testing.T) {
		// The contract cannot make a peer send an action it does not know, so
		// the fixture answer of one action is withdrawn instead: gocpp must
		// answer NotImplemented, and the peer must report it as a CALLERROR.
		if !vc.Client.sends("Heartbeat") {
			t.Skipf("%s does not send Heartbeat", p.name)
		}
		resp.mute("Heartbeat", true)
		defer resp.mute("Heartbeat", false)
		r := refCall(t, name, "", "Heartbeat", json.RawMessage(`{}`))
		if r.CallError == nil || (r.CallError.Code != "NotImplemented" && r.CallError.Code != "NotSupported") {
			failKnown(t, "expected NotImplemented/NotSupported, got %s", r.Raw)
		}
	})
}

func lastReq(r *recorder, action string) string {
	req, _ := r.last(action)
	return string(req)
}

func wantCallError(t *testing.T, err error) {
	t.Helper()
	var ce *ocppj.CallError
	if err == nil {
		failKnown(t, "expected a CALLERROR, got a result")
		return
	}
	if !asOcppjCallError(err, &ce) {
		failKnown(t, "expected a CALLERROR, got %v", err)
		return
	}
	switch ce.Code {
	case ocppj.ErrorCodeNotImplemented, ocppj.ErrorCodeNotSupported:
	default:
		failKnown(t, "expected NotImplemented or NotSupported, got %s (%s)", ce.Code, ce.Description)
	}
}

func asOcppjCallError(err error, out **ocppj.CallError) bool {
	for e := err; e != nil; {
		if ce, ok := e.(*ocppj.CallError); ok {
			*out = ce
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

var _ = strings.TrimSpace
