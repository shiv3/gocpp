// Command testbackend is the processing tier used by the chaos tests: a
// csms.Server behind the gRPC link with a few handlers whose behaviour the
// charger controls through the vendor name (hold, slow, ...). It writes one line
// per handled request to stdout so tests can count what ran where.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type bootReq struct {
	ChargePointVendor string `json:"chargePointVendor"`
	ChargePointModel  string `json:"chargePointModel"`
}
type bootResp struct {
	Status      string `json:"status"`
	CurrentTime string `json:"currentTime"`
	Interval    int    `json:"interval"`
}
type hbReq struct{}
type hbResp struct {
	CurrentTime string `json:"currentTime"`
}

func main() {
	id := flag.String("id", "be", "backend id")
	pool := flag.String("pool", "", "backend pool")
	targets := flag.String("gateways", "", "comma separated gateway link addresses (gRPC), or the NATS server URL")
	linkKind := flag.String("link", "grpc", "grpc|nats")
	redisAddr := flag.String("redis", "", "registry address")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	var be gateway.BackendLink
	if *linkKind == "nats" {
		nc, err := linknats.Connect(*targets, "testbackend-"+*id)
		must(err)
		defer nc.Close()
		nb, err := linknats.NewBackend(nc, linknats.WithHelloInterval(500*time.Millisecond), linknats.WithDrainTimeout(20*time.Second))
		must(err)
		defer nb.Close()
		be = nb
	} else {
		be = linkgrpc.NewBackend(linkgrpc.StaticTargets(strings.Split(*targets, ",")...),
			linkgrpc.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
			linkgrpc.WithResolveInterval(100*time.Millisecond), linkgrpc.WithDrainTimeout(20*time.Second))
	}
	opts := []csms.Option{csms.WithGatewayLink(be), csms.WithSubProtocols("ocpp1.6"),
		csms.WithBackendInfo(gateway.BackendInfo{ID: *id, Pool: *pool, Version: "test"})}
	if *redisAddr != "" {
		opts = append(opts, csms.WithSessionRegistry(registryredis.New(redis.NewClient(&redis.Options{Addr: *redisAddr}))))
	}
	srv := csms.NewServer(opts...)
	must(csms.On(srv, ocppj.Message[bootReq, bootResp]{Action: "BootNotification", Direction: ocppj.SentByCP},
		func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
			fmt.Printf("HANDLED BootNotification %s %s %s\n", *id, c.ID(), r.ChargePointVendor)
			switch r.ChargePointVendor {
			case "hold": // a handler that runs until the process is killed
				time.Sleep(time.Hour)
			case "slow":
				time.Sleep(700 * time.Millisecond)
			}
			return bootResp{Status: "Accepted", CurrentTime: time.Now().UTC().Format(time.RFC3339), Interval: 60}, nil
		}))
	must(csms.On(srv, ocppj.Message[hbReq, hbResp]{Action: "Heartbeat", Direction: ocppj.SentByCP},
		func(ctx context.Context, c *csms.Conn, _ hbReq) (hbResp, error) {
			fmt.Printf("HANDLED Heartbeat %s %s\n", *id, c.ID())
			return hbResp{CurrentTime: time.Now().UTC().Format(time.RFC3339)}, nil
		}))
	fmt.Println("READY", *id)
	if err := srv.ServeGateway(ctx); err != nil {
		fmt.Println("DRAIN", err)
	}
	fmt.Println("EXIT", *id)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}
