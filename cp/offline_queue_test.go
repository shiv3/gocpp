package cp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestClientOfflineQueueFlushesFIFOOnReconnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := NewClient("CP_1", "ws://example.invalid", WithOfflineQueue(4))
	defer client.Close()

	firstDone := make(chan callOutcome, 1)
	secondDone := make(chan callOutcome, 1)
	go func() {
		resp, err := CallRaw(context.Background(), client, "First", []byte(`{}`))
		firstDone <- callOutcome{payload: resp, err: err}
	}()
	require.Eventually(t, func() bool {
		return client.queueLen() == 1
	}, time.Second, time.Millisecond)
	go func() {
		resp, err := CallRaw(context.Background(), client, "Second", []byte(`{}`))
		secondDone <- callOutcome{payload: resp, err: err}
	}()
	require.Eventually(t, func() bool {
		return client.queueLen() == 2
	}, time.Second, time.Millisecond)

	f := transport.NewFakeWS("ocpp1.6")
	dconn := dispatcher.NewConn(client.id, f, client.cfg.dispatcher, client.reg)
	dconn.Start(context.Background())
	defer func() { _ = dconn.Close(nil) }()
	client.publishConn(dconn)

	first := <-f.Sent()
	require.Equal(t, "First", callAction(t, first))
	f.Inject([]byte(`[3,"` + callMsgID(t, first) + `",{"status":"first"}]`))
	firstResult := <-firstDone
	require.NoError(t, firstResult.err)
	require.JSONEq(t, `{"status":"first"}`, string(firstResult.payload))

	second := <-f.Sent()
	require.Equal(t, "Second", callAction(t, second))
	f.Inject([]byte(`[3,"` + callMsgID(t, second) + `",{"status":"second"}]`))
	secondResult := <-secondDone
	require.NoError(t, secondResult.err)
	require.JSONEq(t, `{"status":"second"}`, string(secondResult.payload))
}

func TestClientOfflineQueueFull(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := NewClient("CP_1", "ws://example.invalid", WithOfflineQueue(1))
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	firstErr := make(chan error, 1)
	go func() {
		_, err := CallRaw(ctx, client, "First", []byte(`{}`))
		firstErr <- err
	}()
	require.Eventually(t, func() bool {
		return client.queueLen() == 1
	}, time.Second, time.Millisecond)

	_, err := CallRaw(context.Background(), client, "Second", []byte(`{}`))
	require.ErrorIs(t, err, ocppj.ErrQueueFull)

	cancel()
	require.ErrorIs(t, <-firstErr, context.Canceled)
}

func TestClientOfflineQueueContextCancelDrains(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := NewClient("CP_1", "ws://example.invalid", WithOfflineQueue(2))
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := CallRaw(ctx, client, "Heartbeat", []byte(`{}`))
		errCh <- err
	}()
	require.Eventually(t, func() bool {
		return client.queueLen() == 1
	}, time.Second, time.Millisecond)

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
	require.Eventually(t, func() bool {
		return client.queueLen() == 0
	}, time.Second, time.Millisecond)
}

func TestClientOfflineQueueDisabledFailsFast(t *testing.T) {
	client := NewClient("CP_1", "ws://example.invalid")

	_, err := CallRaw(context.Background(), client, "Heartbeat", []byte(`{}`))
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

// Default: a CALL that was already in-flight when the connection drops must
// fail (OCPP gives no idempotency guarantee), not be silently re-sent.
func TestClientOfflineQueueInFlightFailsOnDisconnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := NewClient("CP_1", "ws://example.invalid", WithOfflineQueue(4))
	defer client.Close()

	done := make(chan callOutcome, 1)
	go func() {
		resp, err := CallRaw(context.Background(), client, "First", []byte(`{}`))
		done <- callOutcome{payload: resp, err: err}
	}()
	require.Eventually(t, func() bool { return client.queueLen() == 1 }, time.Second, time.Millisecond)

	f := transport.NewFakeWS("ocpp1.6")
	dconn := dispatcher.NewConn(client.id, f, client.cfg.dispatcher, client.reg)
	dconn.Start(context.Background())
	client.publishConn(dconn)

	// Wait until the call is in-flight (frame sent), then drop the connection
	// before any response arrives.
	first := <-f.Sent()
	require.Equal(t, "First", callAction(t, first))
	_ = dconn.Close(nil)

	res := <-done
	require.ErrorIs(t, res.err, ocppj.ErrConnClosed)
	require.Eventually(t, func() bool { return client.queueLen() == 0 }, time.Second, time.Millisecond)
}

// Opt-in: WithRetryInFlightCalls re-sends an in-flight CALL after reconnect.
func TestClientOfflineQueueInFlightRetriesWhenEnabled(t *testing.T) {
	defer goleak.VerifyNone(t)
	client := NewClient("CP_1", "ws://example.invalid", WithOfflineQueue(4), WithRetryInFlightCalls())
	defer client.Close()

	done := make(chan callOutcome, 1)
	go func() {
		resp, err := CallRaw(context.Background(), client, "First", []byte(`{}`))
		done <- callOutcome{payload: resp, err: err}
	}()
	require.Eventually(t, func() bool { return client.queueLen() == 1 }, time.Second, time.Millisecond)

	// First connection: call goes in-flight, then drops before a response.
	f1 := transport.NewFakeWS("ocpp1.6")
	c1 := dispatcher.NewConn(client.id, f1, client.cfg.dispatcher, client.reg)
	c1.Start(context.Background())
	client.publishConn(c1)
	sent1 := <-f1.Sent()
	require.Equal(t, "First", callAction(t, sent1))
	_ = c1.Close(nil)

	// Reconnect: the same call must be re-sent and can now be answered.
	f2 := transport.NewFakeWS("ocpp1.6")
	c2 := dispatcher.NewConn(client.id, f2, client.cfg.dispatcher, client.reg)
	c2.Start(context.Background())
	defer func() { _ = c2.Close(nil) }()
	client.publishConn(c2)

	sent2 := <-f2.Sent()
	require.Equal(t, "First", callAction(t, sent2))
	f2.Inject([]byte(`[3,"` + callMsgID(t, sent2) + `",{"status":"ok"}]`))
	res := <-done
	require.NoError(t, res.err)
	require.JSONEq(t, `{"status":"ok"}`, string(res.payload))
}

type callOutcome struct {
	payload []byte
	err     error
}

func callMsgID(t *testing.T, raw []byte) string {
	t.Helper()
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &arr))
	var id string
	require.NoError(t, json.Unmarshal(arr[1], &id))
	return id
}

func callAction(t *testing.T, raw []byte) string {
	t.Helper()
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &arr))
	var action string
	require.NoError(t, json.Unmarshal(arr[2], &action))
	return action
}
