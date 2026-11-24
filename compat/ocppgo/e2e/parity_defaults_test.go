// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/compat"
	"github.com/lorenzodonini/ocpp-go/ws"
)

func skipLegacy(t *testing.T) {
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("a feature of the gocpp runtime")
	}
}

func dialRaw(t *testing.T, port int, id string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/%s", port, id),
		http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// An idle connection is dropped without a close frame (ocpp-go: close code 1006
// at the charge point) unless compat.IdleCloseFrame is selected (1000).
func TestIdleCloseDefaultsToDrop(t *testing.T) {
	skipLegacy(t)
	for _, tc := range []struct {
		name string
		mode compat.IdleClose
		want int
	}{
		{"drop (default)", compat.IdleCloseDrop, websocket.CloseAbnormalClosure},
		{"close frame", compat.IdleCloseFrame, websocket.CloseNormalClosure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCentral(t, func(s *ws.Server) {
				tc2 := ws.NewServerTimeoutConfig()
				tc2.PingWait = 500 * time.Millisecond
				s.SetTimeoutConfig(tc2)
				if tc.mode != compat.IdleCloseDrop {
					compat.SetIdleClose(s, tc.mode)
				}
			})
			conn := dialRaw(t, c.port, "CP-IDLE")
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _, err := conn.ReadMessage()
			ce, ok := err.(*websocket.CloseError)
			if !ok || ce.Code != tc.want {
				t.Fatalf("read error %v, want close code %d", err, tc.want)
			}
		})
	}
}

// There is no inbound size limit (ocpp-go v0.19.0); compat.SetMaxMessageSize adds one.
func TestMessageSizeDefaultUnlimited(t *testing.T) {
	skipLegacy(t)
	big := `[2,"big","DataTransfer",{"vendorId":"v","data":"` + strings.Repeat("a", 2<<20) + `"}]`
	for _, tc := range []struct {
		name  string
		limit int64
		want  string
	}{
		{"unlimited (default)", 0, "reply"},
		{"limited", 1 << 20, "closed 1009"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newCentral(t, func(s *ws.Server) {
				if tc.limit > 0 {
					compat.SetMaxMessageSize(s, tc.limit)
				}
			})
			conn := dialRaw(t, c.port, "CP-BIG")
			if err := conn.WriteMessage(websocket.TextMessage, []byte(big)); err != nil && tc.limit == 0 {
				t.Fatal(err)
			}
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, data, err := conn.ReadMessage()
			got := "reply"
			if err != nil {
				got = "closed"
				if ce, ok := err.(*websocket.CloseError); ok {
					got = fmt.Sprintf("closed %d", ce.Code)
				}
			}
			if got != tc.want {
				t.Fatalf("got %s (%s / %v), want %s", got, data, err, tc.want)
			}
		})
	}
}
