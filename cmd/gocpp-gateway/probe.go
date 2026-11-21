package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// probeFrames are the BootNotification payloads per subprotocol.
var probeBoot = map[string]string{
	"ocpp1.6":   `{"chargePointVendor":"gocpp-probe","chargePointModel":"probe"}`,
	"ocpp2.0.1": `{"reason":"PowerUp","chargingStation":{"model":"probe","vendorName":"gocpp-probe"}}`,
	"ocpp2.1":   `{"reason":"PowerUp","chargingStation":{"model":"probe","vendorName":"gocpp-probe"}}`,
}

// probe is the pre-flight check run as a startup probe: for each OCPP version
// it upgrades against the gateway's own listener with the reserved identity
// "$probe-<id>", sends BootNotification and Heartbeat and expects CALLRESULTs.
// A gateway that cannot complete this (no backend, bad TLS, broken link) never
// receives a charger. The backend must answer the probe identity without
// business side effects.
func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	url := fs.String("url", env("PROBE_URL", "ws://127.0.0.1:8080/ocpp/"), "WebSocket URL prefix of the gateway's listener")
	id := fs.String("id", env("ID", "gateway"), "gateway id (identity is $probe-<id>)")
	versions := fs.String("versions", env("SUBPROTOCOLS", "ocpp1.6"), "comma separated subprotocols to check")
	timeout := fs.Duration("timeout", 10*time.Second, "overall timeout")
	insecure := fs.Bool("insecure", false, "do not verify the gateway's TLS certificate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	hc := &http.Client{}
	if *insecure {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in for self-signed pre-flight
	}
	for _, v := range strings.Split(*versions, ",") {
		boot, ok := probeBoot[v]
		if !ok {
			return fmt.Errorf("probe: no BootNotification payload for %q", v)
		}
		if err := probeOne(ctx, hc, *url+"$probe-"+*id, v, boot); err != nil {
			return fmt.Errorf("probe %s: %w", v, err)
		}
	}
	return nil
}

func probeOne(ctx context.Context, hc *http.Client, url, sub, boot string) error {
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{sub}, HTTPClient: hc})
	if err != nil {
		return fmt.Errorf("upgrade: %w", err)
	}
	defer func() { _ = c.CloseNow() }()
	for i, call := range []struct{ action, payload string }{{"BootNotification", boot}, {"Heartbeat", "{}"}} {
		msgID := fmt.Sprintf("probe-%d", i)
		frame := fmt.Sprintf(`[2,%q,%q,%s]`, msgID, call.action, call.payload)
		if err := c.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			return fmt.Errorf("%s: %w", call.action, err)
		}
		_, data, err := c.Read(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", call.action, err)
		}
		if !strings.HasPrefix(string(data), `[3,"`+msgID+`"`) {
			return fmt.Errorf("%s: unexpected answer %s", call.action, data)
		}
	}
	return c.Close(websocket.StatusNormalClosure, "probe done")
}
