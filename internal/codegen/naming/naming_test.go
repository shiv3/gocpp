package naming

import "testing"

func TestExport(t *testing.T) {
	cases := map[string]string{
		"chargePointVendor": "ChargePointVendor",
		"connectorId":       "ConnectorID",
		"evseId":            "EVSEID",
		"iccid":             "ICCID",
		"idTag":             "IDTag",
		"idTagInfo":         "IDTagInfo",
		"imsi":              "IMSI",
		"meterValue":        "MeterValue",
		"ocppCsmsUrl":       "OCPPCsmsURL",
		"transactionId":     "TransactionID",
		"url":               "URL",
	}
	for in, want := range cases {
		if got := Export(in); got != want {
			t.Errorf("Export(%q) = %q, want %q", in, got, want)
		}
	}
}
