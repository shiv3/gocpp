package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj/signing"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v21client "github.com/shiv3/gocpp/v21/client"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	v21p "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func cpSignerCert(t *testing.T) (*signing.Signer, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "CP_1"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	s, err := signing.NewSigner(key, cert)
	require.NoError(t, err)
	return s, cert
}

func TestE2E_21SignedBootNotification(t *testing.T) {
	signer, cert := cpSignerCert(t)

	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"), csms.WithVerifier(signing.NewVerifier(cert)), csms.WithRequireSignature(true))
	gotReason := make(chan string, 1)
	require.NoError(t, csms.On(srv, v21p.BootNotification, func(_ context.Context, _ *csms.Conn, req v21msg.BootNotificationRequest) (v21msg.BootNotificationResponse, error) {
		gotReason <- req.Reason
		return v21msg.BootNotificationResponse{CurrentTime: time.Now().UTC(), Interval: 300, Status: "Accepted"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1"), cp.WithSigner(signer))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	resp, err := v21client.NewCP(client).BootNotification(ctx, v21msg.BootNotificationRequest{
		Reason:          "PowerUp",
		ChargingStation: v21msg.ChargingStationType{Model: "M1", VendorName: "Acme"},
	})
	require.NoError(t, err)
	require.Equal(t, "Accepted", resp.Status)
	require.Equal(t, "PowerUp", <-gotReason)
}

func TestE2E_21SignedBootNotificationRejectedWithoutTrust(t *testing.T) {
	signer, _ := cpSignerCert(t)
	_, otherCert := cpSignerCert(t) // CSMS trusts a different cert

	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"), csms.WithVerifier(signing.NewVerifier(otherCert)), csms.WithRequireSignature(true))
	require.NoError(t, csms.On(srv, v21p.BootNotification, func(_ context.Context, _ *csms.Conn, _ v21msg.BootNotificationRequest) (v21msg.BootNotificationResponse, error) {
		return v21msg.BootNotificationResponse{CurrentTime: time.Now().UTC(), Interval: 300, Status: "Accepted"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1"), cp.WithSigner(signer))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	_, err := v21client.NewCP(client).BootNotification(ctx, v21msg.BootNotificationRequest{
		Reason:          "PowerUp",
		ChargingStation: v21msg.ChargingStationType{Model: "M1", VendorName: "Acme"},
	})
	require.Error(t, err) // CSMS replies with a SecurityError CallError
}

func TestE2E_21SignedNotifyPeriodicEventStream(t *testing.T) {
	signer, cert := cpSignerCert(t)
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"), csms.WithVerifier(signing.NewVerifier(cert)), csms.WithRequireSignature(true))
	got := make(chan v21msg.NotifyPeriodicEventStream, 1)
	require.NoError(t, csms.OnSend(srv, v21p.NotifyPeriodicEventStream, func(_ context.Context, _ *csms.Conn, req v21msg.NotifyPeriodicEventStream) error {
		got <- req
		return nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1"), cp.WithSigner(signer))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	err := v21client.NewCP(client).NotifyPeriodicEventStream(ctx, v21msg.NotifyPeriodicEventStream{
		ID: 7, Pending: 0, Basetime: time.Now().UTC(),
		Data: []v21msg.StreamDataElementType{{T: decimal.NewFromInt(0), V: "230.0"}},
	})
	require.NoError(t, err)
	select {
	case r := <-got:
		require.EqualValues(t, 7, r.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("CSMS did not receive the signed SEND")
	}
}
