package transport

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// handshake runs one TLS handshake over loopback TCP and reports whether the
// client resumed a session. The client reads once so that it processes the TLS
// 1.3 session ticket the server sends after the handshake.
func handshake(t *testing.T, srvCfg, cliCfg *tls.Config) (resumed bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	errc := make(chan error, 1)
	go func() {
		raw, err := ln.Accept()
		if err != nil {
			errc <- err
			return
		}
		defer raw.Close()
		s := tls.Server(raw, srvCfg)
		if err := s.Handshake(); err != nil {
			errc <- err
			return
		}
		_, err = s.Write([]byte("x")) // wakes the client's read
		errc <- err
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	c := tls.Client(raw, cliCfg)
	require.NoError(t, c.Handshake())
	resumed = c.ConnectionState().DidResume
	buf := make([]byte, 1)
	_, _ = c.Read(buf)
	_ = c.Close()
	require.NoError(t, <-errc)
	return resumed
}

func TestSharedTicketKeysResumeAcrossProcesses(t *testing.T) {
	cert := testCert(t)
	secret := []byte("shared secret")
	newSrv := func() *tls.Config {
		cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
		stop := func() {}
		stop = RotateSharedTicketKeys(cfg, secret, time.Hour)
		t.Cleanup(stop)
		return cfg
	}
	a, b := newSrv(), newSrv()
	cli := &tls.Config{ServerName: "localhost", InsecureSkipVerify: true, ClientSessionCache: tls.NewLRUClientSessionCache(4), MinVersion: tls.VersionTLS13} //nolint:gosec // test certificate

	require.False(t, handshake(t, a, cli))
	// A client holding a ticket from pod A resumes on pod B.
	require.True(t, handshake(t, b, cli))

	// Without the shared secret the other pod cannot decrypt it.
	other := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	require.False(t, handshake(t, other, cli))
}

func TestSharedTicketKeysWindows(t *testing.T) {
	secret := []byte("s")
	now := time.Unix(1_000_000, 0)
	k := SharedTicketKeys(secret, now, time.Hour)
	require.Len(t, k, 3)
	require.NotEqual(t, k[0], k[1])
	require.Equal(t, k, SharedTicketKeys(secret, now.Add(time.Minute), time.Hour), "same window, same keys")
	next := SharedTicketKeys(secret, now.Add(time.Hour), time.Hour)
	require.Equal(t, k[0], next[1], "the previous window's key stays accepted")
	require.Equal(t, k[2], next[0])
}
