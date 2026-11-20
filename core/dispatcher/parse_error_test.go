package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_MalformedCallAnsweredWithFormationViolation(t *testing.T) {
	for _, tc := range []struct {
		sub, code string
	}{
		{"ocpp1.6", "FormationViolation"},
		{"ocpp2.0.1", "FormatViolation"},
	} {
		t.Run(tc.sub, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			f := transport.NewFakeWS(tc.sub)
			c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
			c.Start(context.Background())
			defer func() { _ = c.Close(nil) }()

			f.Inject([]byte(`[2,"id1","Heartbeat"]`))

			sent := <-f.Sent()
			var arr []any
			require.NoError(t, json.Unmarshal(sent, &arr))
			require.Equal(t, []any{float64(4), "id1", tc.code}, arr[:3])
		})
	}
}

func TestConn_CallErrorWithoutDetailsResolvesPendingCall(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.CallTimeout = 10 * time.Second
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	errCh := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		errCh <- err
	}()
	raw := <-f.Sent()
	f.Inject([]byte(`[4,"` + callID(t, raw) + `","InternalError","boom"]`))

	select {
	case err := <-errCh:
		var ce *ocppj.CallError
		require.True(t, errors.As(err, &ce), "got %v", err)
		require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)
		require.Equal(t, "boom", ce.Description)
	case <-time.After(2 * time.Second):
		t.Fatal("pending call was not resolved by the 4-element CallError")
	}
}

// OCPP 2.0.1 / 2.1 Part 4 §4.2.3: a frame with an unsupported MessageTypeId is
// answered with CALLERROR MessageTypeNotSupported. OCPP 1.6J has no such code and
// the frame stays ignored.
func TestConn_UnknownMessageTypeAnswered(t *testing.T) {
	for _, sub := range []string{"ocpp2.0.1", "ocpp2.1"} {
		t.Run(sub, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			f := transport.NewFakeWS(sub)
			c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
			c.Start(context.Background())
			defer func() { _ = c.Close(nil) }()

			f.Inject([]byte(`[9,"id9","Heartbeat",{}]`))

			select {
			case sent := <-f.Sent():
				var arr []any
				require.NoError(t, json.Unmarshal(sent, &arr))
				require.Equal(t, []any{float64(4), "id9", "MessageTypeNotSupported"}, arr[:3])
			case <-time.After(2 * time.Second):
				t.Fatal("no CALLERROR for the unknown message type")
			}
		})
	}
}

func TestConn_UnknownMessageTypeIgnoredOnV16(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[9,"id9","Heartbeat",{}]`))
	// A following malformed CALL is answered; if the unknown frame had produced a
	// reply it would arrive first.
	f.Inject([]byte(`[2,"id2","Heartbeat"]`))

	select {
	case sent := <-f.Sent():
		var arr []any
		require.NoError(t, json.Unmarshal(sent, &arr))
		require.Equal(t, "id2", arr[1], "the unknown frame must not be answered on 1.6")
	case <-time.After(2 * time.Second):
		t.Fatal("no reply to the follow-up frame")
	}
}
