package dispatcher

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/ocppj/signing"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func testSigner(t *testing.T) (*signing.Signer, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cs"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	s, err := signing.NewSigner(key, cert)
	require.NoError(t, err)
	return s, cert
}

func TestDoCallSignsWhenSignerSet(t *testing.T) {
	defer goleak.VerifyNone(t)
	signer, _ := testSigner(t)
	cfg := DefaultConfig()
	cfg.Signer = signer
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	go func() { _, _ = DoCall(context.Background(), c, "Heartbeat", []byte(`{}`)) }()

	raw := <-f.Sent()
	fr, err := ocppj.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, ocppj.Call, fr.Type)
	require.True(t, fr.Signed)
	require.Equal(t, "Heartbeat", fr.Action)
	inner, err := signing.UnwrapPayload(fr.Payload)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(inner))
}

func TestDoSendSignsWhenSignerSet(t *testing.T) {
	defer goleak.VerifyNone(t)
	signer, _ := testSigner(t)
	cfg := DefaultConfig()
	cfg.Signer = signer
	f := transport.NewFakeWS("ocpp2.1")
	c := NewConn("CP_1", f, cfg, NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	require.NoError(t, DoSend(context.Background(), c, "NotifyPeriodicEventStream", []byte(`{"id":1}`)))

	raw := <-f.Sent()
	fr, err := ocppj.Parse(raw)
	require.NoError(t, err)
	require.Equal(t, ocppj.Send, fr.Type)
	require.True(t, fr.Signed)
	require.Equal(t, "NotifyPeriodicEventStream", fr.Action)
	inner, err := signing.UnwrapPayload(fr.Payload)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":1}`, string(inner))
}
