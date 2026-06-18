package ocppj

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeSignedCall(t *testing.T) {
	raw, err := EncodeSignedCall("m1", "BootNotification", []byte(`{"protected":"p","payload":"pl","signature":"s"}`))
	require.NoError(t, err)
	require.JSONEq(t, `[2,"m1","BootNotification-Signed",{"protected":"p","payload":"pl","signature":"s"}]`, string(raw))
}

func TestEncodeSignedSend(t *testing.T) {
	raw, err := EncodeSignedSend("m2", "NotifyPeriodicEventStream", []byte(`{"protected":"p","payload":"pl","signature":"s"}`))
	require.NoError(t, err)
	require.JSONEq(t, `[6,"m2","NotifyPeriodicEventStream-Signed",{"protected":"p","payload":"pl","signature":"s"}]`, string(raw))
}

func TestParseSignedCall(t *testing.T) {
	raw := []byte(`[2,"m1","BootNotification-Signed",{"protected":"p","payload":"pl","signature":"s"}]`)
	f, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, Call, f.Type)
	require.True(t, f.Signed)
	require.Equal(t, "BootNotification", f.Action)
	require.JSONEq(t, `{"protected":"p","payload":"pl","signature":"s"}`, string(f.Payload))
}

func TestParseSignedSend(t *testing.T) {
	raw := []byte(`[6,"m2","NotifyPeriodicEventStream-Signed",{"protected":"p","payload":"pl","signature":"s"}]`)
	f, err := Parse(raw)
	require.NoError(t, err)
	require.Equal(t, Send, f.Type)
	require.True(t, f.Signed)
	require.Equal(t, "NotifyPeriodicEventStream", f.Action)
}

func TestParseUnsignedNotMarkedSigned(t *testing.T) {
	f, err := Parse([]byte(`[2,"m1","BootNotification",{}]`))
	require.NoError(t, err)
	require.False(t, f.Signed)
	require.Equal(t, "BootNotification", f.Action)
}
