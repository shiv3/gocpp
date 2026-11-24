package dispatcher

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
)

// FuzzConnInbound feeds arbitrary bytes to a live connection as one inbound
// frame. Whatever arrives, the connection must neither panic nor wedge: the
// CALL that follows the fuzzed frame is still answered.
func FuzzConnInbound(f *testing.F) {
	for _, s := range []string{
		`[2,"a","Heartbeat",{}]`,
		`[2,"a","Heartbeat"]`,
		`[2,"a","BootNotification",{"chargePointVendor":"V","chargePointModel":"M"}]`,
		`[3,"a",{}]`,
		`[3,"unknown-id",{"status":"Accepted"}]`,
		`[4,"a","InternalError","x",{}]`,
		`[4,"a","InternalError","x"]`,
		`[5,"a","FormatViolation","x",{}]`,
		`[6,"a","Notify",{}]`,
		`[9,"a","Heartbeat",{}]`,
		`[2,"","Heartbeat",{}]`,
		`[2,"a","Heartbeat-Signed",{"payload":"x"}]`,
		`{}`, `[]`, `null`, ``, `[2`, "\xff\xfe", `[2,"a","A",{"k":[[[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]]}]`,
	} {
		f.Add([]byte(s), uint8(0))
	}
	subs := []string{"ocpp1.6", "ocpp2.0.1", "ocpp2.1"}
	f.Fuzz(func(t *testing.T, raw []byte, sel uint8) {
		fw := transport.NewFakeWS(subs[int(sel)%len(subs)])
		c := NewConn("CP_FUZZ", fw, DefaultConfig(), NewHandlerRegistry())
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()

		fw.Inject(append([]byte(nil), raw...))
		const probe = "zz-probe-0001"
		fw.Inject([]byte(`[2,"` + probe + `","NoSuchAction",{}]`))

		deadline := time.After(5 * time.Second)
		for {
			select {
			case out := <-fw.Sent():
				if bytes.Contains(out, []byte(probe)) {
					return
				}
			case <-deadline:
				t.Fatalf("connection stopped answering after inbound frame %q", raw)
			}
		}
	})
}
