// Command migration-after is the gocpp rewrite of a minimal lorenzodonini/ocpp-go
// CSMS handling BootNotification and Heartbeat — see
// docs/migration-from-lorenzodonini.md.
package main

import (
	"context"
	"log"
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/csms"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
)

// handler mirrors ocpp-go's CentralSystemHandler interface: implement the
// messages you care about and embed UnimplementedCSMSHandler for the rest.
type handler struct{ v16h.UnimplementedCSMSHandler }

func (handler) OnBootNotification(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
	slog.Info("boot", "cp", c.ID(), "vendor", req.ChargePointVendor)
	return v16msg.BootNotificationResponse{
		Status:      v16msg.RegistrationStatusAccepted,
		CurrentTime: time.Now(),
		Interval:    300,
	}, nil
}

func (handler) OnHeartbeat(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
	return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
}

func main() {
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithLogger(slog.Default()),
	)

	if err := v16h.RegisterCSMS(srv, handler{}); err != nil {
		log.Fatal(err)
	}

	log.Println("CSMS listening on :8080 at /ocpp/{cpId}")
	log.Fatal(srv.ListenAndServe(":8080"))
}
