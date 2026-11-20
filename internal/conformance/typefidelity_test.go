package conformance_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/shiv3/gocpp/core/datatransfer"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	m16 "github.com/shiv3/gocpp/v16/messages"
	v201 "github.com/shiv3/gocpp/v201"
	m201 "github.com/shiv3/gocpp/v201/messages"
	v21 "github.com/shiv3/gocpp/v21"
	m21 "github.com/shiv3/gocpp/v21/messages"
	"github.com/stretchr/testify/require"
)

// Wire shapes that real stations send but the generated types used to lose or
// reject: untyped schema properties, CustomData extensions and 64-bit integers.

func TestDataTransfer201_ObjectPayload(t *testing.T) {
	validator := conformance.MustValidator(t, conformance.SchemaRegistry(v201.RegisterSchemas), "2.0.1", "DataTransfer", "request")
	wire := []byte(`{"vendorId":"v","data":{"a":1,"b":[true,null]}}`)
	require.NoError(t, validator.Validate(wire))

	var req m201.DataTransferRequest
	require.NoError(t, json.Unmarshal(wire, &req))
	require.JSONEq(t, `{"a":1,"b":[true,null]}`, string(req.Data))

	out, err := json.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, string(wire), string(out))

	type payload struct {
		A int   `json:"a"`
		B []any `json:"b"`
	}
	p, err := datatransfer.Unmarshal[payload](req.Data)
	require.NoError(t, err)
	require.Equal(t, 1, p.A)
}

func TestDataTransfer21_AnyJSONValue(t *testing.T) {
	validator := conformance.MustValidator(t, conformance.SchemaRegistry(v21.RegisterSchemas), "2.1", "DataTransfer", "request")
	for _, data := range []string{`"text"`, `42`, `true`, `{"k":"v"}`, `[1,2]`} {
		wire := []byte(`{"vendorId":"v","data":` + data + `}`)
		require.NoError(t, validator.Validate(wire), data)
		var req m21.DataTransferRequest
		require.NoError(t, json.Unmarshal(wire, &req), data)
		out, err := json.Marshal(req)
		require.NoError(t, err)
		require.JSONEq(t, string(wire), string(out), data)
	}

	// Absent data stays absent (omitempty on json.RawMessage).
	out, err := json.Marshal(m21.DataTransferRequest{VendorID: "v"})
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"v"}`, string(out))
}

func TestDataTransfer16_StaysString(t *testing.T) {
	s := "payload"
	out, err := json.Marshal(m16.DataTransferRequest{VendorID: "v", Data: &s})
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"v","data":"payload"}`, string(out))
}

const customDataWire = `{"vendorId":"x","foo":1,"bar":{"a":true}}`

func TestCustomData201_RoundTripsUnknownProperties(t *testing.T) {
	validator := conformance.MustValidator(t, conformance.SchemaRegistry(v201.RegisterSchemas), "2.0.1", "DataTransfer", "request")
	wire := []byte(`{"vendorId":"v","customData":` + customDataWire + `}`)
	require.NoError(t, validator.Validate(wire))

	var req m201.DataTransferRequest
	require.NoError(t, json.Unmarshal(wire, &req))
	require.NotNil(t, req.CustomData)
	require.Equal(t, "x", req.CustomData.VendorID)
	require.JSONEq(t, `1`, string(req.CustomData.Extra["foo"]))
	require.JSONEq(t, `{"a":true}`, string(req.CustomData.Extra["bar"]))
	require.NotContains(t, req.CustomData.Extra, "vendorId")

	out, err := json.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, string(wire), string(out))
}

func TestCustomData21_RoundTripsUnknownProperties(t *testing.T) {
	validator := conformance.MustValidator(t, conformance.SchemaRegistry(v21.RegisterSchemas), "2.1", "DataTransfer", "response")
	wire := []byte(`{"status":"Accepted","customData":` + customDataWire + `}`)
	require.NoError(t, validator.Validate(wire))

	var resp m21.DataTransferResponse
	require.NoError(t, json.Unmarshal(wire, &resp))
	require.NotNil(t, resp.CustomData)
	require.JSONEq(t, `1`, string(resp.CustomData.Extra["foo"]))

	out, err := json.Marshal(resp)
	require.NoError(t, err)
	require.JSONEq(t, string(wire), string(out))
}

func TestCustomData_PlainValueAndDefinedFieldWins(t *testing.T) {
	out, err := json.Marshal(m201.CustomDataType{VendorID: "x"})
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"x"}`, string(out))

	out, err = json.Marshal(m201.CustomDataType{VendorID: "x", Extra: map[string]json.RawMessage{"vendorId": json.RawMessage(`"y"`)}})
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"x"}`, string(out))
}

func TestInt64TransactionID16(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)

	meter := []byte(`{"connectorId":1,"transactionId":9223372036854775807,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":"1"}]}]}`)
	require.NoError(t, conformance.MustValidator(t, reg, "1.6", "MeterValues", "request").Validate(meter))
	var mv m16.MeterValuesRequest
	require.NoError(t, json.Unmarshal(meter, &mv))
	require.NotNil(t, mv.TransactionID)
	require.Equal(t, int64(math.MaxInt64), *mv.TransactionID)

	stop := []byte(`{"transactionId":9223372036854775807,"meterStop":10,"timestamp":"2026-01-01T00:00:00Z"}`)
	require.NoError(t, conformance.MustValidator(t, reg, "1.6", "StopTransaction", "request").Validate(stop))
	var st m16.StopTransactionRequest
	require.NoError(t, json.Unmarshal(stop, &st))
	require.Equal(t, int64(math.MaxInt64), st.TransactionID)

	out, err := json.Marshal(st)
	require.NoError(t, err)
	require.JSONEq(t, string(stop), string(out))
}
