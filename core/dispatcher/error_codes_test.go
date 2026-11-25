package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func callErrorCode(t *testing.T, f *transport.FakeWS, code string, validate bool) []any {
	t.Helper()
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
		if code == "raw" {
			return nil, &RawCallError{Err: ocppj.NewCallError("Bogus", "boom", nil), Wire: "Bogus"}
		}
		return nil, ocppj.NewCallError(ocppj.ErrorCode(code), "boom", nil)
	})
	cfg := DefaultConfig()
	cfg.ValidateErrorCodes = validate
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))
	var arr []any
	require.NoError(t, json.Unmarshal(<-f.Sent(), &arr))
	return arr
}

func TestValidateErrorCodes_OutboundReplacesUndefinedCode(t *testing.T) {
	defer goleak.VerifyNone(t)
	// Off by default: whatever the handler chose goes out.
	arr := callErrorCode(t, transport.NewFakeWS("ocpp1.6"), "Bogus", false)
	require.Equal(t, "Bogus", arr[2])

	// On: undefined codes become GenericError, original kept in the description.
	arr = callErrorCode(t, transport.NewFakeWS("ocpp1.6"), "Bogus", true)
	require.Equal(t, "GenericError", arr[2])
	require.Contains(t, arr[3], "original code Bogus")

	arr = callErrorCode(t, transport.NewFakeWS("ocpp1.6"), "raw", true)
	require.Equal(t, "GenericError", arr[2])

	// 2.x-only codes are not valid on 1.6, and vice versa for the spellings.
	arr = callErrorCode(t, transport.NewFakeWS("ocpp1.6"), "RpcFrameworkError", true)
	require.Equal(t, "GenericError", arr[2])
	arr = callErrorCode(t, transport.NewFakeWS("ocpp2.0.1"), "RpcFrameworkError", true)
	require.Equal(t, "RpcFrameworkError", arr[2])

	// A code WireCode spells per version stays as it is.
	arr = callErrorCode(t, transport.NewFakeWS("ocpp1.6"), "FormatViolation", true)
	require.Equal(t, "FormationViolation", arr[2])
	arr = callErrorCode(t, transport.NewFakeWS("ocpp2.0.1"), "FormationViolation", true)
	require.Equal(t, "FormatViolation", arr[2])
}

func TestValidateErrorCodes_InboundLoggedNotChanged(t *testing.T) {
	defer goleak.VerifyNone(t)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.ValidateErrorCodes = true
	cfg.Logger = slog.New(slog.NewTextHandler(&buf, nil))
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	errCh := make(chan error, 1)
	go func() { _, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`)); errCh <- err }()
	raw := <-f.Sent()
	f.Inject([]byte(`[4,"` + callID(t, raw) + `","FormatViolation","x"]`))
	select {
	case err := <-errCh:
		var ce *ocppj.CallError
		require.ErrorAs(t, err, &ce)
		require.Equal(t, ocppj.ErrorCode("FormatViolation"), ce.Code, "delivered unchanged")
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	require.Contains(t, buf.String(), "does not define")
}
