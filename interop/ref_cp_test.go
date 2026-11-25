//go:build interop

package interop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/cp"
)

// TestInterop_GocppCP runs gocpp as the charge point against each reference
// CSMS (a peer implementation acting as the central system), with CALLs from
// both ends.
func TestInterop_GocppCP(t *testing.T) {
	for _, p := range selectedPeers() {
		p := p
		t.Run(p.name, func(t *testing.T) {
			caps := loadCaps(t, p)
			for _, v := range selectedVersions() {
				v := v
				t.Run(v.ocpp, func(t *testing.T) {
					vc, ok := caps.Versions[v.ocpp]
					if !ok || !vc.Server.Supported {
						t.Skipf("%s has no CSMS for OCPP %s", p.name, v.ocpp)
					}
					runGocppCP(t, p, v, vc)
				})
			}
		})
	}
}

func runGocppCP(t *testing.T, p peerSpec, v versionSpec, vc versionCaps) {
	fx := loadFixture(t, p, v)
	station := fx.Stations[0].ID
	port := freePort(t)
	name := startContainer(t, p.imageRef(), nil, "server", "--version", v.ocpp, "--port", fmt.Sprint(port))

	resp := newResponder(fx, v.ocpp)
	rec := newRecorder()
	client := cp.NewClient(station, fmt.Sprintf("ws://127.0.0.1:%d/ocpp/%s", port, station), cp.WithSubProtocols(v.sub),
		// gocpp offers permessage-deflate by default; it is exercised in the
		// compression scenario, so the message scenarios isolate framing.
		cp.WithCompression(transport.CompressionDisabled))
	cp.OnAny(client, func(ctx context.Context, action string, payload []byte) ([]byte, error) {
		out, err := resp.answer("station", action, payload)
		if err != nil {
			rec.note(action, append([]byte(nil), payload...), nil)
			return nil, rawErr(err)
		}
		rec.note(action, append([]byte(nil), payload...), out)
		return out, nil
	})
	if err := registerTypedCP(client, v.ocpp, resp, rec); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("gocpp station could not connect to %s: %v", p.name, err)
	}
	defer client.Close()

	weak := !vc.fixtureAnswers()

	// gocpp station -> peer CSMS
	expect := newExpectation(fx, v.ocpp)
	for _, sn := range fx.sampleNames() {
		s := fx.Samples[sn]
		if s.From != "station" {
			continue
		}
		sn, s := sn, s
		t.Run("gocpp-to-peer/"+sn, func(t *testing.T) {
			if !vc.Server.handles(s.Action) && !weak {
				t.Skipf("%s does not declare %s", p.name, s.Action)
			}
			want, err := expect.answer("csms", s.Action, s.Payload)
			if err != nil {
				t.Skipf("fixture has no answer: %v", err)
			}
			cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer ccancel()
			out, err := callFromCP(cctx, client, v.ocpp, s.Action, s.Payload)
			strict := vc.Server.handles(s.Action) || !weak
			if err != nil {
				var ce *ocppj.CallError
				if weak && !strict && asOcppjCallError(err, &ce) {
					t.Skipf("peer does not process %s: %s (%s)", s.Action, ce.Code, ce.Description)
				}
				failKnown(t, "gocpp CALL failed: %v", err)
				return
			}
			if strict {
				if err := jsonSubset(want, out); err != nil {
					failKnown(t, "answer differs from the fixture: %v (got %s)", err, out)
				}
			}
		})
	}

	// peer CSMS -> gocpp station
	for _, sn := range fx.sampleNames() {
		s := fx.Samples[sn]
		if s.From != "csms" {
			continue
		}
		sn, s := sn, s
		t.Run("peer-to-gocpp/"+sn, func(t *testing.T) {
			if !vc.Server.sends(s.Action) {
				t.Skipf("%s cannot send %s", p.name, s.Action)
			}
			want, err := newExpectation(fx, v.ocpp).answer("station", s.Action, s.Payload)
			if err != nil {
				t.Skipf("fixture has no answer: %v", err)
			}
			r := refCall(t, name, station, s.Action, s.Payload)
			if r.CallError != nil || !r.OK {
				failKnown(t, "peer got %s (gocpp saw %s)", r.Raw, lastReq(rec, s.Action))
				return
			}
			if !weak || vc.Server.handles(s.Action) {
				if err := jsonSubset(want, r.Response); err != nil {
					failKnown(t, "answer differs from the fixture: %v (peer parsed %s)", err, r.Response)
					return
				}
			}
			req, _ := rec.last(s.Action)
			if err := jsonSubset(s.Payload, req); err != nil {
				failKnown(t, "gocpp received a different payload than sent: %v (got %s)", err, req)
			}
		})
	}

	t.Run("unknown-action/gocpp-to-peer", func(t *testing.T) {
		cctx, ccancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ccancel()
		_, err := cp.CallRaw(cctx, client, "NoSuchAction", []byte(`{}`))
		wantCallError(t, err)
	})
	t.Run("unanswered-action/peer-to-gocpp", func(t *testing.T) {
		if !vc.Server.sends("ClearCache") {
			t.Skipf("%s cannot send ClearCache", p.name)
		}
		resp.mute("ClearCache", true)
		defer resp.mute("ClearCache", false)
		r := refCall(t, name, station, "ClearCache", json.RawMessage(`{}`))
		if r.CallError == nil || (r.CallError.Code != "NotImplemented" && r.CallError.Code != "NotSupported") {
			failKnown(t, "expected NotImplemented/NotSupported, got %s", r.Raw)
		}
	})
}
