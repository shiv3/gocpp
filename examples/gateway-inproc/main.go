// Command gateway-inproc runs a CSMS split into a gateway (connection tier) and a
// processing tier in one process, joined by the in-process link. The handlers are
// the same typed code as examples/csms-minimal; only the wiring differs. With
// the gRPC link (addons/link-grpc) the two halves become separate processes
// (see cmd/gocpp-gateway).
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway/inproc"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
)

type handler struct{ v16h.UnimplementedCSMSHandler }

func (handler) OnBootNotification(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
	ref, _ := c.GatewaySession()
	slog.Info("boot", "cp", c.ID(), "vendor", req.ChargePointVendor, "gateway", ref.GatewayID, "epoch", ref.Epoch)
	return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
}

func (handler) OnHeartbeat(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
	return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := inproc.New()

	// Connection tier: terminates WebSockets, runs no handlers.
	gw := csms.NewGatewayServer(hub.Gateway(),
		csms.WithGatewayID("gw-1"),
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithLogger(slog.Default()),
	)

	// Processing tier: the handlers, schema options, authenticator, callbacks.
	be := csms.NewServer(
		csms.WithGatewayLink(hub.Backend()),
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnConnect(func(c *csms.Conn) { slog.Info("connected", "cp", c.ID()) }),
		csms.WithOnDisconnect(func(c *csms.Conn, err error) { slog.Info("disconnected", "cp", c.ID(), "cause", err) }),
	)
	if err := v16h.RegisterCSMS(be, handler{}); err != nil {
		log.Fatal(err)
	}
	go func() { _ = be.ServeGateway(ctx) }()

	srv := &http.Server{Addr: ":8080", Handler: gw.Handler()}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = gw.Shutdown(sctx)
		_ = srv.Shutdown(sctx)
	}()
	log.Println("gateway listening on :8080 at /ocpp/{cpId}")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
