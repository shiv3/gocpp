// native r07: the same frames as r07, against a gocpp native CSMS (generated
// int64 fields, payload decoded once into the typed struct), in the default
// lenient schema mode. Also: a handler panic (r04) on the same server.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
)

type handler struct {
	v16h.UnimplementedCSMSHandler
	got chan int64
}

func (h handler) OnStopTransaction(ctx context.Context, c *csms.Conn, req v16msg.StopTransactionRequest) (v16msg.StopTransactionResponse, error) {
	h.got <- req.TransactionID
	return v16msg.StopTransactionResponse{}, nil
}

func (h handler) OnHeartbeat(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
	if c.ID() == "bad" {
		var p *v16msg.HeartbeatResponse
		return *p, nil // application bug: nil pointer dereference
	}
	return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
}

func main() {
	reg := schema.NewRegistry()
	if err := v16.RegisterSchemas(reg); err != nil {
		panic(err)
	}
	h := handler{got: make(chan int64, 1)}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithSchemaRegistry(reg))
	if err := v16h.RegisterCSMS(srv, h); err != nil {
		panic(err)
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	go http.Serve(l, srv.Handler())
	dial := func(id string) *websocket.Conn {
		d := websocket.Dialer{Subprotocols: []string{"ocpp1.6"}}
		c, _, err := d.Dial(fmt.Sprintf("ws://%s/ocpp/%s", l.Addr(), id), nil)
		if err != nil {
			panic(err)
		}
		return c
	}
	fmt.Println("library: gocpp v0.3.3 native API (csms.NewServer, generated v16 handlers, lenient schema)")
	cp := dial("cp1")
	for i, id := range []string{"1234567", "9007199254740993", "1152921504606846977", "9223372036854775807", "0"} {
		_ = cp.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`[2,"m%d","StopTransaction",{"transactionId":%s,"meterStop":0,"timestamp":"2026-11-24T00:00:00Z"}]`, i, id)))
		_ = cp.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, raw, err := cp.ReadMessage()
		select {
		case v := <-h.got:
			mark := "same"
			if fmt.Sprint(v) != id {
				mark = "CHANGED"
			}
			fmt.Printf("wire transactionId %-20s -> handler saw %-20d %s; reply %s\n", id, v, mark, raw)
		case <-time.After(500 * time.Millisecond):
			fmt.Printf("wire transactionId %-20s -> handler NOT called; reply %s (err %v)\n", id, raw, err)
		}
	}
	bad := dial("bad")
	_ = bad.WriteMessage(websocket.TextMessage, []byte(`[2,"b1","Heartbeat",{}]`))
	_ = bad.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := bad.ReadMessage()
	fmt.Printf("handler panic for charge point \"bad\": reply %s (err %v)\n", raw, err)
	_ = cp.WriteMessage(websocket.TextMessage, []byte(`[2,"h1","Heartbeat",{}]`))
	_ = cp.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err = cp.ReadMessage()
	fmt.Printf("other charge point afterwards: reply %s (err %v); process alive\n", raw, err)
}
