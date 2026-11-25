//go:build interop

package interop

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
)

// testPKI is a throwaway CA with a server and a client certificate, written in
// the layout the container contract mounts (server/ and client/ directories).
type testPKI struct {
	dir    string
	caPool *x509.CertPool
	server tls.Certificate
	client tls.Certificate
}

func newTestPKI(t *testing.T, stationID string) *testPKI {
	t.Helper()
	dir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	// RSA keys (the contract allows RSA >= 2048 and ECDSA P-256).
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	must(err)
	caTpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "gocpp interop test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	must(err)
	caCert, err := x509.ParseCertificate(caDER)
	must(err)
	issue := func(serial int64, cn string, server bool) ([]byte, []byte, tls.Certificate) {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		must(err)
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
			BasicConstraintsValid: true, IsCA: false,
			SubjectKeyId: []byte{byte(serial), 1, 2, 3, 4, 5, 6, 7},
		}
		if server {
			tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			tpl.DNSNames = []string{"localhost"}
			tpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		} else {
			tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, caCert, &k.PublicKey, caKey)
		must(err)
		kb, err := x509.MarshalPKCS8PrivateKey(k)
		must(err)
		crt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kb})
		pair, err := tls.X509KeyPair(crt, key)
		must(err)
		return crt, key, pair
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	// libocpp (EVerest) requires the server certificate CN to equal the URL host.
	sc, sk, sp := issue(2, "localhost", true)
	cc, ck, cp := issue(3, stationID, false)
	write := func(sub, name string, b []byte) {
		d := filepath.Join(dir, sub)
		must(os.MkdirAll(d, 0o755))
		must(os.Chmod(d, 0o755))
		// The images run as uid 10001; these are throwaway test keys.
		must(os.WriteFile(filepath.Join(d, name), b, 0o644))
	}
	write("server", "server.crt", sc)
	write("server", "server.key", sk)
	write("server", "ca.crt", caPEM)
	write("client", "ca.crt", caPEM)
	write("client", "client.crt", cc)
	write("client", "client.key", ck)
	must(os.Chmod(dir, 0o755))
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	return &testPKI{dir: dir, caPool: pool, server: sp, client: cp}
}

func (p *testPKI) mount() []string { return []string{"-v", p.dir + ":/tls:ro"} }

func hasProfile(role roleCaps, f featureCaps, n int) bool {
	l := f.SecurityProfiles
	if len(role.SecurityProfiles) > 0 {
		l = role.SecurityProfiles
	}
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}

// gocppAuth is the CSMS-side authenticator of a profile.
func gocppAuth(profile int, fx *fixture) auth.Authenticator {
	switch profile {
	case 3:
		return auth.MTLSFromClientCert(func(cpID string, c *x509.Certificate) (auth.Identity, error) {
			if c.Subject.CommonName != cpID {
				return auth.Identity{}, auth.ErrUnauthorized
			}
			return auth.Identity{CPID: cpID, Method: auth.AuthMethodMTLS}, nil
		})
	default:
		return auth.BasicAuth(func(cpID, password string) (auth.Identity, error) {
			for _, s := range fx.Stations {
				if s.ID == cpID && s.Password == password {
					return auth.Identity{CPID: cpID, Method: auth.AuthMethodBasic}, nil
				}
			}
			return auth.Identity{}, auth.ErrUnauthorized
		})
	}
}

func startGocppCSMSTLS(t *testing.T, v versionSpec, resp *responder, profile int, pki *testPKI, fx *fixture, extra ...csms.Option) *gocppCSMS {
	t.Helper()
	rec := newRecorder()
	opts := append([]csms.Option{csms.WithSubProtocols(v.sub), csms.WithAuthenticator(gocppAuth(profile, fx))}, extra...)
	srv := csms.NewServer(opts...)
	csms.OnAny(srv, rawAnswer(resp, "csms", rec))
	if err := registerTypedCSMS(srv, v.ocpp, resp, rec); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	if profile >= 2 {
		cfg := &tls.Config{Certificates: []tls.Certificate{pki.server}, MinVersion: tls.VersionTLS12}
		if profile == 3 {
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
			cfg.ClientCAs = pki.caPool
		}
		hs.TLSConfig = cfg
		go func() { _ = hs.ServeTLS(l, "", "") }()
	} else {
		go func() { _ = hs.Serve(l) }()
	}
	t.Cleanup(func() { _ = hs.Close(); srv.Close() })
	return &gocppCSMS{srv: srv, http: hs, port: l.Addr().(*net.TCPAddr).Port, rec: rec}
}

func stationTLSArgs(profile int) []string {
	if profile < 2 {
		return nil
	}
	return []string{"--tls-dir", "/tls/client"}
}

// tlsHost is the host name TLS scenarios connect by; the test server
// certificate carries it as CN and SAN (libocpp checks the CN against the URL).
func tlsHost(profile int) string {
	if profile >= 2 {
		return "localhost"
	}
	return "127.0.0.1"
}

func scheme(profile int) string {
	if profile >= 2 {
		return "wss"
	}
	return "ws"
}

// TestInterop_GocppCSMS_Security runs the reference stations against gocpp as
// the CSMS over security profiles 1 (Basic), 2 (TLS + Basic) and 3 (mutual
// TLS), and with permessage-deflate.
func TestInterop_GocppCSMS_Security(t *testing.T) {
	for _, p := range selectedPeers() {
		p := p
		t.Run(p.name, func(t *testing.T) {
			caps := loadCaps(t, p)
			for _, v := range selectedVersions() {
				v := v
				t.Run(v.ocpp, func(t *testing.T) {
					vc, ok := caps.Versions[v.ocpp]
					if !ok || !vc.Client.Supported {
						t.Skipf("%s has no station for OCPP %s", p.name, v.ocpp)
					}
					fx := loadFixture(t, p, v)
					station := fx.Stations[0].ID
					for _, profile := range []int{1, 2, 3} {
						profile := profile
						t.Run(fmt.Sprintf("profile%d", profile), func(t *testing.T) {
							if !hasProfile(vc.Client, vc.Features, profile) {
								t.Skipf("%s station does not declare security profile %d", p.name, profile)
							}
							pki := newTestPKI(t, station)
							resp := newResponder(fx, v.ocpp)
							srv := startGocppCSMSTLS(t, v, resp, profile, pki, fx)
							url := fmt.Sprintf("%s://%s:%d/ocpp", scheme(profile), tlsHost(profile), srv.port)
							args := append([]string{"client", "run", "--version", v.ocpp, "--url", url, "--security-profile", fmt.Sprint(profile)}, stationTLSArgs(profile)...)
							name := startContainer(t, p.imageRef(), pki.mount(), args...)
							conn := srv.waitConn(t, station)
							t.Run("peer-to-gocpp/boot", func(t *testing.T) {
								s := firstSample(fx, "station", "BootNotification")
								r := refCall(t, name, "", s.Action, s.Payload)
								if !r.OK {
									failKnown(t, "peer got %s", r.Raw)
								}
							})
							t.Run("gocpp-to-peer/first", func(t *testing.T) {
								s, ok := firstHandled(fx, "csms", vc.Client)
								if !ok {
									t.Skip("no CSMS sample the peer handles")
								}
								cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
								defer cancel()
								if _, err := callFromCSMS(cctx, conn, v.ocpp, s.Action, s.Payload); err != nil {
									failKnown(t, "gocpp CALL %s failed: %v", s.Action, err)
								}
							})
							if profile <= 2 {
								t.Run("wrong-credentials-refused", func(t *testing.T) {
									// An identity the fixture does not list makes the station send a password
									// of its own, which gocpp's authenticator must refuse.
									a := append([]string{"run", "--rm", "--network", "host"}, pki.mount()...)
									a = append(a, p.imageRef(), "client", "run", "--version", v.ocpp, "--url", url, "--station", "NOT-A-STATION-01",
										"--security-profile", fmt.Sprint(profile), "--hold-ms", "1500", "--connect-timeout-ms", "5000")
									a = append(a, stationTLSArgs(profile)...)
									_, se, err := docker(60*time.Second, a...)
									if err == nil {
										failKnown(t, "a station with unknown credentials was admitted (stderr %s)", tail(se, 400))
									}
									if _, ok := srv.srv.Get("NOT-A-STATION-01"); ok {
										t.Fatal("gocpp kept a connection for the unknown station")
									}
								})
							}
						})
					}
					t.Run("compression", func(t *testing.T) {
						resp := newResponder(fx, v.ocpp)
						srv := startGocppCSMS(t, v, resp, csms.WithCompression(transport.CompressionContextTakeover))
						args := []string{"client", "run", "--version", v.ocpp, "--compression", "--url", fmt.Sprintf("ws://127.0.0.1:%d/ocpp", srv.port)}
						name := startContainer(t, p.imageRef(), nil, args...)
						conn := srv.waitConn(t, station)
						s := firstSample(fx, "station", "BootNotification")
						if r := refCall(t, name, "", s.Action, s.Payload); !r.OK {
							failKnown(t, "peer got %s", r.Raw)
						}
						if hs, ok := firstHandled(fx, "csms", vc.Client); ok {
							cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
							defer cancel()
							if _, err := callFromCSMS(cctx, conn, v.ocpp, hs.Action, hs.Payload); err != nil {
								failKnown(t, "gocpp CALL %s failed: %v", hs.Action, err)
							}
						}
					})
				})
			}
		})
	}
}

// TestInterop_GocppCP_Security runs gocpp as the station against the reference
// CSMSs over security profiles 1 to 3 and with permessage-deflate.
func TestInterop_GocppCP_Security(t *testing.T) {
	for _, p := range selectedPeers() {
		p := p
		t.Run(p.name, func(t *testing.T) {
			caps := loadCaps(t, p)
			for _, v := range selectedVersions() {
				v := v
				t.Run(v.ocpp, func(t *testing.T) {
					vc, ok := caps.Versions[v.ocpp]
					if !ok || !vc.Server.Supported {
						t.Skipf("%s has no CSMS for OCPP %s", p.name, v.ocpp)
					}
					fx := loadFixture(t, p, v)
					station := fx.Stations[0].ID
					password := fx.Stations[0].Password
					for _, profile := range []int{1, 2, 3} {
						profile := profile
						t.Run(fmt.Sprintf("profile%d", profile), func(t *testing.T) {
							if !hasProfile(vc.Server, vc.Features, profile) {
								t.Skipf("%s CSMS does not declare security profile %d", p.name, profile)
							}
							pki := newTestPKI(t, station)
							port := freePort(t)
							args := []string{"server", "--version", v.ocpp, "--port", fmt.Sprint(port), "--security-profile", fmt.Sprint(profile)}
							if profile >= 2 {
								args = append(args, "--tls-dir", "/tls/server")
							}
							startContainer(t, p.imageRef(), pki.mount(), args...)
							url := fmt.Sprintf("%s://%s:%d/ocpp/%s", scheme(profile), tlsHost(profile), port, station)
							opts := []cp.Option{cp.WithSubProtocols(v.sub)}
							if profile <= 2 {
								opts = append(opts, cp.WithBasicAuth(station, password))
							}
							if profile >= 2 {
								cfg := &tls.Config{RootCAs: pki.caPool, MinVersion: tls.VersionTLS12}
								if profile == 3 {
									cfg.Certificates = []tls.Certificate{pki.client}
								}
								opts = append(opts, cp.WithTLSConfig(cfg))
							}
							client := cp.NewClient(station, url, opts...)
							ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
							defer cancel()
							if err := client.Connect(ctx); err != nil {
								failKnown(t, "gocpp station could not connect: %v", err)
								return
							}
							defer client.Close()
							t.Run("gocpp-to-peer/boot", func(t *testing.T) {
								s := firstSample(fx, "station", "BootNotification")
								cctx, c2 := context.WithTimeout(context.Background(), 20*time.Second)
								defer c2()
								out, err := callFromCP(cctx, client, v.ocpp, s.Action, s.Payload)
								if err != nil {
									failKnown(t, "gocpp CALL failed: %v", err)
									return
								}
								want, _ := newExpectation(fx, v.ocpp).answer("csms", s.Action, s.Payload)
								if err := jsonSubset(want, out); err != nil {
									failKnown(t, "answer differs from the fixture: %v (got %s)", err, out)
								}
							})
							if profile <= 2 {
								t.Run("wrong-password-refused", func(t *testing.T) {
									bad := []cp.Option{cp.WithSubProtocols(v.sub), cp.WithBasicAuth(station, "wrong-password-0000")}
									if profile == 2 {
										bad = append(bad, cp.WithTLSConfig(&tls.Config{RootCAs: pki.caPool, MinVersion: tls.VersionTLS12}))
									}
									c := cp.NewClient(station, url, bad...)
									cctx, c2 := context.WithTimeout(context.Background(), 10*time.Second)
									defer c2()
									if err := c.Connect(cctx); err == nil {
										c.Close()
										failKnown(t, "the CSMS admitted a wrong password")
									}
								})
							}
						})
					}
					t.Run("compression", func(t *testing.T) {
						port := freePort(t)
						name := startContainer(t, p.imageRef(), nil, "server", "--version", v.ocpp, "--port", fmt.Sprint(port))
						client := cp.NewClient(station, fmt.Sprintf("ws://127.0.0.1:%d/ocpp/%s", port, station),
							cp.WithSubProtocols(v.sub), cp.WithCompression(transport.CompressionContextTakeover))
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						if err := client.Connect(ctx); err != nil {
							failKnown(t, "gocpp station could not connect: %v", err)
							return
						}
						defer client.Close()
						s := firstSample(fx, "station", "BootNotification")
						if _, err := cp.CallRaw(ctx, client, s.Action, s.Payload); err != nil {
							failKnown(t, "gocpp CALL failed: %v", err)
							return
						}
						negotiated := "unreported"
						for _, e := range containerEvents(name) {
							if e["event"] == "connection-opened" {
								if b, ok := e["compression"].(bool); ok {
									negotiated = fmt.Sprint(b)
								}
							}
						}
						t.Logf("permessage-deflate negotiated (as the peer reports it): %s", negotiated)
						if negotiated == "false" && vc.Features.Compression {
							failKnown(t, "the CSMS declares compression but did not negotiate it")
						}
					})
				})
			}
		})
	}
}

func firstSample(fx *fixture, from, action string) sample {
	for _, n := range fx.sampleNames() {
		if s := fx.Samples[n]; s.From == from && s.Action == action {
			return s
		}
	}
	panic("fixture has no " + from + " sample of " + action)
}

// firstHandled returns the first sample sent by from whose action the other end
// declares it handles.
func firstHandled(fx *fixture, from string, other roleCaps) (sample, bool) {
	for _, n := range fx.sampleNames() {
		if s := fx.Samples[n]; s.From == from && other.handles(s.Action) {
			return s, true
		}
	}
	return sample{}, false
}

var _ = json.Marshal
