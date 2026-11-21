// Command testbackend is the processing tier used by the chaos tests: a
// csms.Server behind the gRPC link with a few handlers whose behaviour the
// charger controls through the vendor name (hold, slow, ...). It writes one line
// per handled request to stdout so tests can count what ran where.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
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
type resetReq struct {
	Type string `json:"type"`
}
type resetResp struct {
	Status string `json:"status"`
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
	failHB := flag.Bool("fail-heartbeat", false, "answer every Heartbeat with an InternalError CALLERROR (an error-injecting canary)")
	admin := flag.String("admin", "", "listen address of the test control API (GET /call?cp=ID issues a CSMS Reset to the charger from this backend)")
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
		csms.WithBackendInfo(gateway.BackendInfo{ID: *id, Pool: *pool, Version: "test"}),
		csms.WithOnOrphanResult(func(cpID, callID, action string, payload []byte, err error) {
			fmt.Printf("ORPHAN %s %s %s %s err=%v\n", *id, cpID, callID, action, err)
		})}
	if *redisAddr != "" {
		opts = append(opts, csms.WithSessionRegistry(registryredis.New(redis.NewClient(&redis.Options{Addr: *redisAddr}))))
	}
	srv := csms.NewServer(opts...)
	must(csms.On(srv, ocppj.Message[bootReq, bootResp]{Action: "BootNotification", Direction: ocppj.SentByCP},
		func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
			fmt.Printf("HANDLED BootNotification %s %s %s\n", *id, c.ID(), r.ChargePointVendor)
			switch r.ChargePointVendor {
			case "busy": // the charger starts a transaction: hold its routing (gocpp.busy)
				_ = c.SetAttrs(ctx, map[string]string{"gocpp.busy": "1"})
			case "idle":
				_ = c.SetAttrs(ctx, map[string]string{"gocpp.busy": ""})
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
			if *failHB {
				return hbResp{}, ocppj.NewCallError(ocppj.ErrorCodeInternalError, "injected", nil)
			}
			return hbResp{CurrentTime: time.Now().UTC().Format(time.RFC3339)}, nil
		}))
	if *admin != "" {
		l, err := net.Listen("tcp", *admin)
		must(err)
		mux := http.NewServeMux()
		mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
			cpID := r.URL.Query().Get("cp")
			cctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			h, err := srv.Lookup(cctx, cpID)
			if err != nil {
				fmt.Fprintf(w, "ERR not_sent %v\n", err)
				return
			}
			fmt.Printf("CALLING Reset %s %s\n", *id, cpID)
			resp, err := csms.Call(cctx, h, ocppj.Message[resetReq, resetResp]{Action: "Reset", Direction: ocppj.SentByCSMS}, resetReq{Type: "Soft"})
			if err != nil {
				fmt.Fprintf(w, "ERR %s %v\n", gateway.SentOf(err), err)
				return
			}
			fmt.Fprintf(w, "OK %s\n", resp.Status)
		})
		go func() { _ = http.Serve(l, mux) }()
	}
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
