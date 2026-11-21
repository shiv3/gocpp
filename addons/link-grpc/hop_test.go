package linkgrpc_test

import (
	"context"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	"github.com/shiv3/gocpp/gateway"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// echoUpstream answers every CALL with a fixed payload: the link and nothing else.
type echoUpstream struct{ reply []byte }

func (e echoUpstream) Connect(context.Context, gateway.ConnectRequest) (gateway.ConnectReply, error) {
	return gateway.ConnectReply{}, nil
}

func (e echoUpstream) Handle(context.Context, gateway.Envelope) (gateway.Reply, error) {
	return gateway.Reply{Payload: e.reply}, nil
}
func (e echoUpstream) Notify(context.Context, gateway.Envelope) error { return nil }

// hopRig is a gateway link and one backend over loopback gRPC.
func hopRig(tb testing.TB) (g *linkgrpc.Gateway, stop func()) {
	tb.Helper()
	g = linkgrpc.NewGateway()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	gs := grpc.NewServer()
	g.Register(gs)
	go func() { _ = gs.Serve(lis) }()
	be := linkgrpc.NewBackend(linkgrpc.StaticTargets(lis.Addr().String()),
		linkgrpc.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())), linkgrpc.WithResolveInterval(50*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = be.Serve(ctx, echoUpstream{reply: make([]byte, 300)}, gateway.BackendInfo{})
	}()
	deadline := time.Now().Add(5 * time.Second)
	for !g.Available(gateway.DefaultPool) {
		if time.Now().After(deadline) {
			tb.Fatal("backend did not attach")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return g, func() { cancel(); <-done; gs.Stop() }
}

func hopEnv() gateway.Envelope {
	return gateway.Envelope{
		Proto: 1, Kind: gateway.KindCall, Action: "MeterValues", MsgID: "m1", Payload: make([]byte, 300),
		Session: gateway.SessionRef{CPID: "CP_1", GatewayID: "gw", Boot: "b", Epoch: 1},
	}
}

// BenchmarkGRPCHop is one inbound CALL forwarded over the gRPC link (300-byte
// payloads, loopback, trivial backend): the cost of the link alone.
func BenchmarkGRPCHop(b *testing.B) {
	g, stop := hopRig(b)
	defer stop()
	ctx := context.Background()
	b.Run("sequential", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := g.Handle(ctx, hopEnv()); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("parallel-256", func(b *testing.B) {
		b.ReportAllocs()
		b.SetParallelism(256)
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, err := g.Handle(ctx, hopEnv()); err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

// TestGateGRPCHopLatency is the gate for the gRPC hop: at 5,000 msg/s open loop
// over loopback the added latency must stay far below the design's 2 ms p99 for
// gateway plus hop (design 2.7). The bound here is 5 ms because CI hosts are
// shared; the measured value is logged.
func TestGateGRPCHopLatency(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing gate: not under -short or -race")
	}
	g, stop := hopRig(t)
	defer stop()
	const rate, dur = 5000, 3 * time.Second
	n := int(rate * dur / time.Second)
	lat := make([]time.Duration, n)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs int
	start := time.Now()
	for i := 0; i < n; i++ {
		// Open loop: send on schedule, not when the previous one returned.
		time.Sleep(time.Until(start.Add(time.Duration(i) * time.Second / rate)))
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			if _, err := g.Handle(context.Background(), hopEnv()); err != nil {
				mu.Lock()
				errs++
				mu.Unlock()
			}
			lat[i] = time.Since(t0)
		}()
	}
	wg.Wait()
	if errs > 0 {
		t.Fatalf("%d of %d requests failed", errs, n)
	}
	slices.Sort(lat)
	p50, p99 := lat[n/2], lat[n*99/100]
	t.Logf("%d msg/s open loop, loopback gRPC hop: p50 %v p99 %v max %v", rate, p50, p99, lat[n-1])
	if p99 > 5*time.Millisecond {
		t.Errorf("p99 %v above the 5 ms gate", p99)
	}
}
