package ocppj

import "testing"

var (
	benchCall       = []byte(`[2,"01JABCDEFGHJKMNPQRSTVWXYZ0","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Available","timestamp":"2026-11-21T01:02:03Z"}]`)
	benchCallResult = []byte(`[3,"01JABCDEFGHJKMNPQRSTVWXYZ0",{"currentTime":"2026-11-21T01:02:03.000Z"}]`)
	benchCallError  = []byte(`[4,"01JABCDEFGHJKMNPQRSTVWXYZ0","NotImplemented","unknown action",{}]`)
)

var sinkFrame Frame
var sinkBytes []byte

func BenchmarkParse(b *testing.B) {
	for _, c := range []struct {
		name string
		raw  []byte
	}{{"Call", benchCall}, {"CallResult", benchCallResult}, {"CallError", benchCallError}} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				f, err := Parse(c.raw)
				if err != nil {
					b.Fatal(err)
				}
				sinkFrame = f
			}
		})
	}
}

func BenchmarkEncodeCall(b *testing.B) {
	payload := []byte(`{"connectorId":1,"errorCode":"NoError","status":"Available"}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, _ := EncodeCall("01JABCDEFGHJKMNPQRSTVWXYZ0", "StatusNotification", payload)
		sinkBytes = out
	}
}

func BenchmarkEncodeCallResult(b *testing.B) {
	payload := []byte(`{"currentTime":"2026-11-21T01:02:03.000Z"}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, _ := EncodeCallResult("01JABCDEFGHJKMNPQRSTVWXYZ0", payload)
		sinkBytes = out
	}
}

func BenchmarkEncodeCallError(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		out, _ := EncodeCallError("01JABCDEFGHJKMNPQRSTVWXYZ0", "NotImplemented", "unknown action", nil)
		sinkBytes = out
	}
}

func BenchmarkNewMsgID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewMsgID()
	}
}

func BenchmarkNewMsgIDParallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = NewMsgID()
		}
	})
}
