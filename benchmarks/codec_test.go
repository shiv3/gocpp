package benchmarks

import (
	"encoding/json"
	"testing"

	v16msg "github.com/shiv3/gocpp/v16/messages"
)

func BenchmarkBootNotification_Marshal(b *testing.B) {
	req := v16msg.BootNotificationRequest{ChargePointVendor: "Acme", ChargePointModel: "Model-X"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(req); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBootNotification_Unmarshal(b *testing.B) {
	data := []byte(`{"chargePointVendor":"Acme","chargePointModel":"Model-X"}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req v16msg.BootNotificationRequest
		if err := json.Unmarshal(data, &req); err != nil {
			b.Fatal(err)
		}
	}
}
