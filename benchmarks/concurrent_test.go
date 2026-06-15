package benchmarks

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
)

func BenchmarkConcurrent_200Conns(b *testing.B) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	_ = csms.On(srv, benchMsg, func(ctx context.Context, c *csms.Conn, req benchReq) (benchResp, error) {
		return benchResp{Status: "ok"}, nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const conns = 200
	clients := make([]*cp.Client, conns)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := 0; i < conns; i++ {
		url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_" + itoa(i)
		c := cp.NewClient("CP_"+itoa(i), url, cp.WithSubProtocols("ocpp1.6"))
		if err := c.Connect(ctx); err != nil {
			b.Fatal(err)
		}
		clients[i] = c
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(conns)
		for _, c := range clients {
			go func(c *cp.Client) {
				defer wg.Done()
				_, _ = cp.Call(ctx, c, benchMsg, benchReq{V: "x"})
			}(c)
		}
		wg.Wait()
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
