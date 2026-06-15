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
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
)

func main() {
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithLogger(slog.Default()),
	)

	if err := csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		slog.Info("boot", "cp", c.ID(), "vendor", req.ChargePointVendor)
		return v16msg.BootNotificationResponse{
			Status:      v16msg.RegistrationStatusAccepted,
			CurrentTime: time.Now(),
			Interval:    300,
		}, nil
	}); err != nil {
		log.Fatal(err)
	}

	if err := csms.On(srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
		return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
	}); err != nil {
		log.Fatal(err)
	}

	log.Println("CSMS listening on :8080 at /ocpp/{cpId}")
	log.Fatal(srv.ListenAndServe(":8080"))
}
