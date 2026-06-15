package benchmarks

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
)

type benchReq struct {
	V string `json:"v"`
}
type benchResp struct {
	Status string `json:"status"`
}

var benchMsg = ocppj.Message[benchReq, benchResp]{Action: "BenchEcho", Direction: ocppj.SentByCP}

func BenchmarkCallRTT(b *testing.B) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	_ = csms.On(srv, benchMsg, func(ctx context.Context, c *csms.Conn, req benchReq) (benchResp, error) {
		return benchResp{Status: "ok"}, nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_BENCH"
	client := cp.NewClient("CP_BENCH", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		b.Fatal(err)
	}
	defer client.Close()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := cp.Call(ctx, client, benchMsg, benchReq{V: "x"}); err != nil {
			b.Fatal(err)
		}
	}
}
