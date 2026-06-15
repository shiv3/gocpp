package dispatcher

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestDoCall_Success(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), newHandlerRegistry())
	c.Start(context.Background())
	defer c.Close(nil)

	// Reply to whatever Call the test sends with a fixed CallResult.
	go func() {
		raw := <-f.Sent()
		// raw = [2,"<id>","ChangeConfiguration",{...}]
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[3,"` + id + `",{"status":"Accepted"}]`))
	}()

	resp, err := DoCall(context.Background(), c, "ChangeConfiguration",
		[]byte(`{"key":"X","value":"1"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"Accepted"}`, string(resp))
}

func TestDoCall_ConnClosed(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), newHandlerRegistry())
	c.Start(context.Background())
	_ = c.Close(nil)

	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.Error(t, err)
}
