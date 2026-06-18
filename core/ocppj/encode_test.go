package ocppj

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeCall(t *testing.T) {
	raw, err := EncodeCall("abc", "BootNotification", []byte(`{"chargePointVendor":"X"}`))
	require.NoError(t, err)
	require.JSONEq(t, `[2,"abc","BootNotification",{"chargePointVendor":"X"}]`, string(raw))
}

func TestEncodeCallResult(t *testing.T) {
	raw, err := EncodeCallResult("abc", []byte(`{"status":"Accepted"}`))
	require.NoError(t, err)
	require.JSONEq(t, `[3,"abc",{"status":"Accepted"}]`, string(raw))
}

func TestEncodeCallError(t *testing.T) {
	raw, err := EncodeCallError("abc", "NotImplemented", "nope", []byte(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `[4,"abc","NotImplemented","nope",{}]`, string(raw))
}

func TestEncodeSend(t *testing.T) {
	raw, err := EncodeSend("abc", "NotifyPeriodicEventStream", []byte(`{"id":1}`))
	require.NoError(t, err)
	require.JSONEq(t, `[6,"abc","NotifyPeriodicEventStream",{"id":1}]`, string(raw))
}

func TestEncodeCallResultError(t *testing.T) {
	raw, err := EncodeCallResultError("abc", "FormatViolation", "bad", nil)
	require.NoError(t, err)
	require.JSONEq(t, `[5,"abc","FormatViolation","bad",{}]`, string(raw))
}

func TestEncodeParse_RoundTrip(t *testing.T) {
	raw, err := EncodeCall("id1", "Authorize", []byte(`{"idTag":"T"}`))
	require.NoError(t, err)
	f, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, Call, f.Type)
	require.Equal(t, "id1", f.MsgID)
	require.Equal(t, "Authorize", f.Action)
}
