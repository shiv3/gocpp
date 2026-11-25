//go:build interop

package interop

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v21calls "github.com/shiv3/gocpp/v21/calls"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	v21prof "github.com/shiv3/gocpp/v21/profiles"
)

const sendAction = "NotifyPeriodicEventStream"

// TestInterop_Gocpp_Send checks the OCPP 2.1 SEND message type (no answer) in
// both directions, against the peers that declare it.
func TestInterop_Gocpp_Send(t *testing.T) {
	v := versionSpec{"2.1", "ocpp2.1"}
	for _, p := range selectedPeers() {
		p := p
		t.Run(p.name, func(t *testing.T) {
			caps := loadCaps(t, p)
			vc, ok := caps.Versions["2.1"]
			if !ok || !vc.Features.Send {
				t.Skipf("%s does not declare SEND", p.name)
			}
			fx := loadFixture(t, p, v)
			station := fx.Stations[0].ID
			s := firstSample(fx, "station", sendAction)
			t.Run("2.1/peer-to-gocpp/send", func(t *testing.T) {
				if !vc.Client.Supported || !vc.Client.sends(sendAction) {
					t.Skipf("%s station does not send it", p.name)
				}
				srv := startGocppCSMS(t, v, newResponder(fx, "2.1"))
				got := make(chan v21msg.NotifyPeriodicEventStream, 1)
				if err := csms.OnSend(srv.srv, v21prof.NotifyPeriodicEventStream, func(ctx context.Context, c *csms.Conn, req v21msg.NotifyPeriodicEventStream) error {
					got <- req
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				name := startContainer(t, p.imageRef(), nil, "client", "run", "--version", "2.1", "--url", fmt.Sprintf("ws://127.0.0.1:%d/ocpp", srv.port))
				srv.waitConn(t, station)
				r := refCall(t, name, "", sendAction, s.Payload)
				if !r.OK {
					failKnown(t, "peer could not send the SEND: %s", r.Raw)
					return
				}
				select {
				case m := <-got:
					var want v21msg.NotifyPeriodicEventStream
					_ = json.Unmarshal(s.Payload, &want)
					if m.ID != want.ID || len(m.Data) != len(want.Data) {
						failKnown(t, "gocpp received a different SEND: %+v", m)
					}
				case <-time.After(10 * time.Second):
					failKnown(t, "gocpp never received the SEND")
				}
			})
			t.Run("2.1/gocpp-to-peer/send", func(t *testing.T) {
				if !vc.Server.Supported || !vc.Server.handles(sendAction) {
					t.Skipf("%s CSMS does not handle it", p.name)
				}
				port := freePort(t)
				name := startContainer(t, p.imageRef(), nil, "server", "--version", "2.1", "--port", fmt.Sprint(port))
				client := cp.NewClient(station, fmt.Sprintf("ws://127.0.0.1:%d/ocpp/%s", port, station),
					cp.WithSubProtocols(v.sub), cp.WithCompression(transport.CompressionDisabled))
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				if err := client.Connect(ctx); err != nil {
					t.Fatalf("connect: %v", err)
				}
				defer client.Close()
				var msg v21msg.NotifyPeriodicEventStream
				if err := json.Unmarshal(s.Payload, &msg); err != nil {
					t.Fatal(err)
				}
				if err := v21calls.CPNotifyPeriodicEventStream(ctx, client, msg); err != nil {
					failKnown(t, "gocpp SEND failed: %v", err)
					return
				}
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					for _, e := range containerEvents(name) {
						if e["event"] == "send-received" {
							return
						}
					}
					time.Sleep(300 * time.Millisecond)
				}
				failKnown(t, "the peer never reported send-received")
			})
		})
	}
}
