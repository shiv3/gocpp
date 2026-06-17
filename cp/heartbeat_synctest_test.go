//go:build go1.25

package cp

import (
	"context"
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func TestClientHeartbeatSendsCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := NewClient("CP_1", "ws://example.invalid", WithHeartbeatInterval(time.Minute))
		f := transport.NewFakeWS("ocpp1.6")
		cfg := client.cfg.dispatcher
		cfg.PingInterval = 0 // isolate the OCPP Heartbeat from transport keepalive
		cfg.ReadTimeout = 0
		dconn := dispatcher.NewConn(client.id, f, cfg, client.reg)
		dconn.Start(context.Background())
		defer func() { _ = dconn.Close(nil) }()
		client.startHeartbeat(dconn)

		synctest.Wait()
		time.Sleep(time.Minute + time.Second)

		sent := <-f.Sent()
		var frame []json.RawMessage
		require.NoError(t, json.Unmarshal(sent, &frame))
		require.Len(t, frame, 4)
		require.JSONEq(t, `2`, string(frame[0]))
		require.JSONEq(t, `"Heartbeat"`, string(frame[2]))
		require.JSONEq(t, `{}`, string(frame[3]))
	})
}

func TestClientHeartbeatDisabledSendsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := NewClient("CP_1", "ws://example.invalid")
		f := transport.NewFakeWS("ocpp1.6")
		cfg := client.cfg.dispatcher
		cfg.PingInterval = 0 // isolate the OCPP Heartbeat from transport keepalive
		cfg.ReadTimeout = 0
		dconn := dispatcher.NewConn(client.id, f, cfg, client.reg)
		dconn.Start(context.Background())
		defer func() { _ = dconn.Close(nil) }()
		client.startHeartbeat(dconn)

		synctest.Wait()
		time.Sleep(time.Minute + time.Second)
		synctest.Wait()

		select {
		case sent := <-f.Sent():
			t.Fatalf("unexpected heartbeat frame: %s", sent)
		default:
		}
	})
}
