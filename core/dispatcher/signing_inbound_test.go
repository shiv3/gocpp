package dispatcher

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/ocppj/signing"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestInboundSignedCallVerifiedDispatch(t *testing.T) {
	defer goleak.VerifyNone(t)
	signer, cert := testSigner(t)
	cfg := DefaultConfig()
	cfg.Verifier = signing.NewVerifier(cert)
	reg := NewHandlerRegistry()
	got := make(chan []byte, 1)
	reg.Register("Heartbeat", func(_ context.Context, _ *Conn, payload []byte) ([]byte, error) {
		got <- append([]byte(nil), payload...)
		return []byte(`{"currentTime":"2026-01-01T00:00:00Z"}`), nil
	})
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	signed, err := signer.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	require.NoError(t, err)
	raw, err := ocppj.EncodeSignedCall("m1", "Heartbeat", signed)
	require.NoError(t, err)
	f.Inject(raw)

	select {
	case p := <-got:
		require.JSONEq(t, `{}`, string(p)) // handler sees the inner payload
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}
	resp := <-f.Sent()
	fr, _ := ocppj.Parse(resp)
	require.Equal(t, ocppj.CallResult, fr.Type)
}

func TestInboundSignedCallBadSigRequireRejects(t *testing.T) {
	defer goleak.VerifyNone(t)
	signer, _ := testSigner(t)
	_, otherCert := testSigner(t) // verifier trusts a different cert
	cfg := DefaultConfig()
	cfg.Verifier = signing.NewVerifier(otherCert)
	cfg.RequireSignatureVerification = true
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(_ context.Context, _ *Conn, _ []byte) ([]byte, error) {
		return []byte(`{}`), nil
	})
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	signed, _ := signer.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	raw, _ := ocppj.EncodeSignedCall("m1", "Heartbeat", signed)
	f.Inject(raw)

	resp := <-f.Sent()
	fr, _ := ocppj.Parse(resp)
	require.Equal(t, ocppj.MessageTypeCallError, fr.Type)
	require.Equal(t, string(ocppj.ErrorCodeSecurityError), fr.ErrCode)
}

func TestInboundSignedCallNoVerifierUnwraps(t *testing.T) {
	defer goleak.VerifyNone(t)
	signer, _ := testSigner(t)
	cfg := DefaultConfig() // no Verifier
	reg := NewHandlerRegistry()
	got := make(chan []byte, 1)
	reg.Register("Heartbeat", func(_ context.Context, _ *Conn, payload []byte) ([]byte, error) {
		got <- append([]byte(nil), payload...)
		return []byte(`{}`), nil
	})
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	signed, _ := signer.SignPayload("Heartbeat", ocppj.Call, []byte(`{"x":1}`))
	raw, _ := ocppj.EncodeSignedCall("m1", "Heartbeat", signed)
	f.Inject(raw)

	select {
	case p := <-got:
		require.JSONEq(t, `{"x":1}`, string(p))
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestInboundSignedSendVerifiedDispatch(t *testing.T) {
	defer goleak.VerifyNone(t)
	signer, cert := testSigner(t)
	cfg := DefaultConfig()
	cfg.Verifier = signing.NewVerifier(cert)
	reg := NewHandlerRegistry()
	got := make(chan []byte, 1)
	reg.RegisterSend("NotifyPeriodicEventStream", func(_ context.Context, _ *Conn, payload []byte) error {
		got <- append([]byte(nil), payload...)
		return nil
	})
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	signed, _ := signer.SignPayload("NotifyPeriodicEventStream", ocppj.Send, []byte(`{"id":9}`))
	raw, _ := ocppj.EncodeSignedSend("m1", "NotifyPeriodicEventStream", signed)
	f.Inject(raw)

	select {
	case p := <-got:
		require.JSONEq(t, `{"id":9}`, string(p))
	case <-time.After(2 * time.Second):
		t.Fatal("SEND handler not called")
	}
	// A SEND never produces a reply, even when signed/verified.
	select {
	case b := <-f.Sent():
		t.Fatalf("signed SEND must not reply, got %s", b)
	case <-time.After(200 * time.Millisecond):
	}
}
