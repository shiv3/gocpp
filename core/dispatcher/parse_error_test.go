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
