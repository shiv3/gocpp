// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// TestIdleTimeout: a charge point that sends nothing for PingWait is dropped,
// and the application hears about it.
func TestIdleTimeout(t *testing.T) {
	c := newCentral(t, func(s *ws.Server) {
		tc := ws.NewServerTimeoutConfig()
		tc.PingWait = 600 * time.Millisecond
		s.SetTimeoutConfig(tc)
	})
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/CP-I", c.port),
		http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitFor(t, "connect", func() bool { return c.connects.Load() == 1 })
	waitFor(t, "idle disconnect", func() bool { return c.drops.Load() == 1 })
}

// TestTLS: the TLS server constructor serves the same sessions.
func TestTLS(t *testing.T) {
	dir := t.TempDir()
	cert, key := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	writeSelfSigned(t, cert, key)
	pool := x509.NewCertPool()
	pem, _ := os.ReadFile(cert)
	pool.AppendCertsFromPEM(pem)

	c := startCentralWith(t, ws.NewTLSServer(cert, key, nil), nil)
	client := ws.NewTLSClient(&tls.Config{RootCAs: pool})
	cp := ocpp16.NewChargePoint("CP-T", nil, client)
	startMu.Lock()
	err := cp.Start(fmt.Sprintf("wss://localhost:%d", c.port))
	startMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cp.Stop)
	boot, err := cp.BootNotification("m", "v")
	if err != nil || boot.Status != core.RegistrationStatusAccepted {
		t.Fatalf("boot over TLS: %+v %v", boot, err)
	}
	if c.tlsSeen.Load() == 0 {
		t.Fatal("the channel did not carry a TLS connection state")
	}
}

func writeSelfSigned(t *testing.T, certFile, keyFile string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	if err := os.WriteFile(certFile, pemEncode("CERTIFICATE", der), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pemEncode("EC PRIVATE KEY", kb), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pemEncode(typ string, b []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b})
}
