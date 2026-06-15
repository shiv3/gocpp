// Command cp-minimal is a minimal OCPP 1.6 charge point.
package main

import (
	"context"
	"log"
	"time"

	"github.com/shiv3/gocpp/cp"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
)

func main() {
	client := cp.NewClient("CP_1", "ws://localhost:8080/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	boot, err := cp.Call(ctx, client, v16p.BootNotification, v16msg.BootNotificationRequest{
		ChargePointVendor: "Acme", ChargePointModel: "Model-X",
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("boot status: %s, interval: %d", boot.Status, boot.Interval)
}
