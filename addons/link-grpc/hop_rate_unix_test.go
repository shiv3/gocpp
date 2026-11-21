//go:build unix

package linkgrpc_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// BenchmarkGRPCHopRate drives one gateway-backend stream over loopback at a
// fixed message rate, open loop (1,000 independent senders with jittered
// intervals, like chargers), and reports what the write linger trades:
// cpu-us/msg is the CPU time of the whole process (both ends of the link and
// the senders) per round trip, p50-us and p99-us the round-trip latency.
// The linger is set on both ends. A stream below the rate at which a linger
// applies (mean interval above half the linger: 10,000 msg/s for 200 us,
// 4,000 msg/s for 500 us) must show the same numbers as linger=0.
//
//	go test -run '^$' -bench GRPCHopRate -benchtime 100000x ./addons/link-grpc
func BenchmarkGRPCHopRate(b *testing.B) {
	for _, rate := range []int{1000, 5000, 20000, 40000} {
		for _, linger := range []time.Duration{0, 200 * time.Microsecond, 500 * time.Microsecond} {
			b.Run(fmt.Sprintf("rate=%d/linger=%v", rate, linger), func(b *testing.B) {
				g, stop := hopRig(b, linger)
				defer stop()
				const senders = 1000
				interval := time.Duration(senders) * time.Second / time.Duration(rate)
				lat := make([]time.Duration, b.N)
				var next atomic.Int64
				var wg sync.WaitGroup
				var reqs sync.WaitGroup
				var failed atomic.Int64
				b.ResetTimer()
				cpu0 := cpuTime()
				for s := 0; s < senders; s++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						time.Sleep(time.Duration(rand.Int64N(int64(interval))))
						for {
							i := int(next.Add(1)) - 1
							if i >= b.N {
								return
							}
							reqs.Add(1)
							go func() {
								defer reqs.Done()
								t0 := time.Now()
								if _, err := g.Handle(context.Background(), hopEnv()); err != nil {
									failed.Add(1)
								}
								lat[i] = time.Since(t0)
							}()
							// 0.5 to 1.5 intervals: the senders drift apart.
							time.Sleep(interval/2 + time.Duration(rand.Int64N(int64(interval))))
						}
					}()
				}
				wg.Wait()
				reqs.Wait()
				cpu := cpuTime() - cpu0
				b.StopTimer()
				if n := failed.Load(); n > 0 {
					b.Fatalf("%d requests failed", n)
				}
				slices.Sort(lat)
				b.ReportMetric(float64(cpu.Microseconds())/float64(b.N), "cpu-us/msg")
				b.ReportMetric(float64(lat[b.N/2].Microseconds()), "p50-us")
				b.ReportMetric(float64(lat[b.N*99/100].Microseconds()), "p99-us")
				b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msg/s")
			})
		}
	}
}
