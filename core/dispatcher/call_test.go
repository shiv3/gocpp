package dispatcher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestDoCall_Success(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

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
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	_ = c.Close(nil)

	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.Error(t, err)
}

func TestDoCall_StrictSchemaRejectsOutboundRequest(t *testing.T) {
	defer goleak.VerifyNone(t)
	want := errors.New("schema nope")
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		require.Equal(t, ocppj.V16, version)
		require.Equal(t, "Heartbeat", action)
		require.Equal(t, "request", kind)
		return want
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.ErrorIs(t, err, want)
	select {
	case sent := <-f.Sent():
		t.Fatalf("unexpected outbound frame: %s", sent)
	default:
	}
}

func TestDoCall_StrictSchemaRejectsInboundResponse(t *testing.T) {
	defer goleak.VerifyNone(t)
	want := errors.New("schema nope")
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		if kind == "response" {
			require.Equal(t, "Heartbeat", action)
			return want
		}
		return nil
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() {
		raw := <-f.Sent()
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[3,"` + id + `",{"bad":true}]`))
	}()

	_, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.ErrorIs(t, err, want)
}

func TestDoCall_TolerantSchemaLogsAndContinues(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	var logs bytes.Buffer
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeTolerant
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		return errors.New("schema nope " + kind)
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() {
		raw := <-f.Sent()
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[3,"` + id + `",{"currentTime":"2026-06-15T00:00:00Z"}]`))
	}()

	resp, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"currentTime":"2026-06-15T00:00:00Z"}`, string(resp))
	require.Equal(t, 2, metrics.schemaFailures)
	require.Contains(t, logs.String(), "kind=request")
	require.Contains(t, logs.String(), "kind=response")
}

func TestDoCall_LenientUsesReturnedPayloads(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeLenient
	cfg.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
		require.Equal(t, ocppj.V16, version)
		require.Equal(t, "ChangeConfiguration", action)
		switch kind {
		case "request":
			return []byte(`{"status":"Accepted"}`), []string{"enum"}, nil
		case "response":
			return []byte(`{"result":"Accepted"}`), []string{"enum"}, nil
		default:
			return payload, nil, nil
		}
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	sentReq := make(chan []byte, 1)
	go func() {
		raw := <-f.Sent()
		sentReq <- raw
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[3,"` + id + `",{"result":"accepted"}]`))
	}()

	resp, err := DoCall(context.Background(), c, "ChangeConfiguration", []byte(`{"status":"accepted"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"result":"Accepted"}`, string(resp))

	var sentFrame []json.RawMessage
	require.NoError(t, json.Unmarshal(<-sentReq, &sentFrame))
	require.JSONEq(t, `{"status":"Accepted"}`, string(sentFrame[3]))
	require.Equal(t, []string{"enum", "enum"}, metrics.softKeywords)
}

func TestDoCall_SchemaModeOffSkipsValidation(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeOff
	validatorCalls := 0
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		validatorCalls++
		return errors.New("schema nope")
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() {
		raw := <-f.Sent()
		var arr []json.RawMessage
		_ = json.Unmarshal(raw, &arr)
		var id string
		_ = json.Unmarshal(arr[1], &id)
		f.Inject([]byte(`[3,"` + id + `",{}]`))
	}()

	resp, err := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(resp))
	require.Zero(t, validatorCalls)
}

func TestDoCall_SerializeOutboundCallsEnabled(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	firstErr := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "First", []byte(`{}`))
		firstErr <- err
	}()
	first := <-f.Sent()
	firstID := callID(t, first)

	secondErr := make(chan error, 1)
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		_, err := DoCall(context.Background(), c, "Second", []byte(`{}`))
		secondErr <- err
	}()
	<-secondStarted

	// Check len (non-consuming) rather than receiving: require.Never runs the
	// condition in a background goroutine that can outlive the call, and a receive
	// would let that straggler steal the second frame and deadlock <-f.Sent() below.
	require.Never(t, func() bool {
		return len(f.Sent()) > 0
	}, 50*time.Millisecond, time.Millisecond)

	f.Inject([]byte(`[3,"` + firstID + `",{}]`))
	require.NoError(t, <-firstErr)

	second := <-f.Sent()
	secondID := callID(t, second)
	f.Inject([]byte(`[3,"` + secondID + `",{}]`))
	require.NoError(t, <-secondErr)
}

func TestDoCall_SerializeOutboundCallsDisabled(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	firstErr := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "First", []byte(`{}`))
		firstErr <- err
	}()
	first := <-f.Sent()
	firstID := callID(t, first)

	secondErr := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "Second", []byte(`{}`))
		secondErr <- err
	}()
	second := <-f.Sent()
	secondID := callID(t, second)

	f.Inject([]byte(`[3,"` + firstID + `",{}]`))
	f.Inject([]byte(`[3,"` + secondID + `",{}]`))
	require.NoError(t, <-firstErr)
	require.NoError(t, <-secondErr)
}

func TestDoCall_SerializeOutboundCallsContextCancelWhileWaiting(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = true
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	firstErr := make(chan error, 1)
	go func() {
		_, err := DoCall(context.Background(), c, "First", []byte(`{}`))
		firstErr <- err
	}()
	first := <-f.Sent()
	firstID := callID(t, first)

	ctx, cancel := context.WithCancel(context.Background())
	secondErr := make(chan error, 1)
	go func() {
		_, err := DoCall(ctx, c, "Second", []byte(`{}`))
		secondErr <- err
	}()
	cancel()
	require.ErrorIs(t, <-secondErr, context.Canceled)

	select {
	case sent := <-f.Sent():
		t.Fatalf("unexpected second frame while waiting for serialized slot: %s", sent)
	default:
	}

	f.Inject([]byte(`[3,"` + firstID + `",{}]`))
	require.NoError(t, <-firstErr)
}

func callID(t *testing.T, raw []byte) string {
	t.Helper()
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &arr))
	var id string
	require.NoError(t, json.Unmarshal(arr[1], &id))
	return id
}

// readOutboundCall drains f.Sent() until a Call frame ([2,...]) is found and
// returns its msgID. Non-Call frames (e.g. pings) are skipped.
func readOutboundCall(t *testing.T, f *transport.FakeWS) string {
	t.Helper()
	for {
		select {
		case raw := <-f.Sent():
			frame, err := ocppj.Parse(raw)
			if err != nil {
				continue
			}
			if frame.Type == ocppj.Call {
				return frame.MsgID
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for outbound Call frame")
		}
	}
}

func TestDoCallInvalidResultEmitsCallResultError(t *testing.T) {
	defer goleak.VerifyNone(t)

	f := transport.NewFakeWS("ocpp2.1")
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(_ ocppj.Version, _, kind string, _ []byte) error {
		if kind == "response" {
			return errors.New("schema: response invalid")
		}
		return nil
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	errCh := make(chan error, 1)
	go func() {
		_, e := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		errCh <- e
	}()

	// Wait for the outbound CALL and capture its msgID.
	msgID := readOutboundCall(t, f)

	// Inject a bad CALLRESULT — validation should reject it.
	f.Inject([]byte(`[3,"` + msgID + `",{"currentTime":"bad"}]`))

	// DoCall must return an error.
	require.Error(t, <-errCh)

	// A CALLRESULTERROR [5,...] must have been emitted with the same msgID.
	select {
	case raw := <-f.Sent():
		frame, err := ocppj.Parse(raw)
		require.NoError(t, err)
		require.Equal(t, ocppj.MessageTypeCallResultError, frame.Type)
		require.Equal(t, msgID, frame.MsgID)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for CALLRESULTERROR frame")
	}
}

func TestDoCallInvalidResultNoCallResultErrorOnOlderVersion(t *testing.T) {
	defer goleak.VerifyNone(t)

	f := transport.NewFakeWS("ocpp2.0.1")
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(_ ocppj.Version, _, kind string, _ []byte) error {
		if kind == "response" {
			return errors.New("schema: response invalid")
		}
		return nil
	}
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	errCh := make(chan error, 1)
	go func() {
		_, e := DoCall(context.Background(), c, "Heartbeat", []byte(`{}`))
		errCh <- e
	}()

	msgID := readOutboundCall(t, f)

	f.Inject([]byte(`[3,"` + msgID + `",{"currentTime":"bad"}]`))

	// DoCall must still return an error.
	require.Error(t, <-errCh)

	// No CALLRESULTERROR should be emitted for a non-2.1 connection.
	select {
	case extra := <-f.Sent():
		frame, _ := ocppj.Parse(extra)
		if frame.Type == ocppj.MessageTypeCallResultError {
			t.Fatalf("unexpected CALLRESULTERROR frame on non-2.1 connection: %s", extra)
		}
		// Any other frame (unlikely here) is fine to ignore.
	case <-time.After(100 * time.Millisecond):
		// Nothing sent — correct behaviour.
	}
}
