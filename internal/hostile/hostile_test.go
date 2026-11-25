// Package hostile runs a live CSMS against peers that misbehave on purpose and
// checks that well-behaved charge points are unaffected and that nothing leaks
// (TestMain: goleak). The attacks are written at the TCP and WebSocket frame
// level, below what any client library would produce.
package hostile

import (
	"context"
	"encoding/binary"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/splitmode"
	v16client "github.com/shiv3/gocpp/v16/client"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

const maxMsg = 8 << 10

type fixture struct {
	t         *testing.T
	srv       *csms.Server
	ts        *httptest.Server
	addr      string
	heartbeat atomic.Int64
}

// newFixture starts a CSMS with a small message limit and a short read idle
// timeout, so that the attacks that rely on them finish quickly.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{t: t}
	opts := []csms.Option{
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithMaxMessageSize(maxMsg),
		csms.WithWebSocketReadTimeout(2 * time.Second),
	}
	fx.srv = splitmode.NewServer(opts...)
	require.NoError(t, csms.On(fx.srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
		fx.heartbeat.Add(1)
		return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
	}))
	fx.ts = httptest.NewServer(fx.srv.Handler())
	fx.addr = strings.TrimPrefix(fx.ts.URL, "http://")
	t.Cleanup(func() {
		fx.ts.CloseClientConnections()
		fx.ts.Close()
		fx.srv.Close()
	})
	return fx
}

// canary connects a well-behaved charge point and makes a round trip. It fails
// the test when the server no longer serves ordinary peers.
func (fx *fixture) canary(id string) {
	fx.t.Helper()
	client := cp.NewClient(id, "ws://"+fx.addr+"/ocpp/"+id, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(fx.t, client.Connect(ctx), "canary %s could not connect", id)
	defer client.Close()
	_, err := v16client.NewCP(client).Heartbeat(ctx, v16msg.HeartbeatRequest{})
	require.NoError(fx.t, err, "canary %s got no Heartbeat answer", id)
}

// waitNoConns waits until the server no longer holds any of the given connections.
func (fx *fixture) waitNoConns(ids ...string) {
	fx.t.Helper()
	require.Eventually(fx.t, func() bool {
		live := fx.srv.Connections()
		for _, id := range ids {
			if slices.Contains(live, id) {
				return false
			}
		}
		return true
	}, 5*time.Second, 20*time.Millisecond, "connections %v were not released", ids)
}

const heartbeat = `[2,"hb-1","Heartbeat",{}]`

// A peer that never finishes the HTTP request headers ties up one server
// goroutine per connection; others are served meanwhile. (The header timeout
// itself is covered in csms: TestListenAndServe_ReadHeaderTimeout.)
func TestSlowlorisHeaders_OthersServed(t *testing.T) {
	fx := newFixture(t)
	var peers []*rawPeer
	for i := 0; i < 50; i++ {
		nc := dialTCP(t, fx.addr)
		p := &rawPeer{t: t, nc: nc}
		_ = p.sendRaw([]byte("GET /ocpp/slow HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\n"))
		peers = append(peers, p)
		t.Cleanup(func() { _ = nc.Close() })
	}
	fx.canary("CP_CANARY")
	for _, p := range peers {
		p.reset()
	}
	fx.canary("CP_CANARY2")
}

// A peer completes the upgrade and then drips a single frame one byte at a
// time, or stops halfway: the read idle timeout ends it and others are served.
func TestSlowlorisFrame_ClosedByReadTimeout(t *testing.T) {
	fx := newFixture(t)
	p := connect(t, fx.addr, "CP_DRIP")
	whole := frame(true, opText, []byte(heartbeat))
	require.NoError(t, p.sendRaw(whole[:3])) // header and part of the mask, then silence
	fx.canary("CP_CANARY")
	_, closed := p.awaitClosed(8 * time.Second)
	require.True(t, closed, "a half-sent frame must not keep the connection open forever")
	fx.waitNoConns("CP_DRIP")
	fx.canary("CP_CANARY2")
}

// Half-open: the peer upgrades and then neither sends nor answers anything.
func TestHalfOpen_SilentPeerClosed(t *testing.T) {
	fx := newFixture(t)
	for i := 0; i < 20; i++ {
		connect(t, fx.addr, "CP_SILENT_"+string(rune('A'+i)))
	}
	fx.canary("CP_CANARY")
	require.Eventually(t, func() bool { return len(fx.srv.Connections()) == 0 }, 8*time.Second, 50*time.Millisecond,
		"silent peers must be dropped by the read idle timeout")
	fx.canary("CP_CANARY2")
}

// A message above the limit closes that connection with 1009; the next ones are fine.
func TestOversizedFrame_Closes1009(t *testing.T) {
	fx := newFixture(t)
	p := connect(t, fx.addr, "CP_BIG")
	p.send(true, opText, []byte(`[2,"big","DataTransfer",{"vendorId":"v","data":"`+strings.Repeat("x", 4*maxMsg)+`"}]`))
	code, closed := p.awaitClosed(5 * time.Second)
	require.True(t, closed)
	require.Equal(t, 1009, code)
	fx.waitNoConns("CP_BIG")
	fx.canary("CP_CANARY")
}

// A frame header that announces 2^63-1 bytes is refused without allocating them.
func TestOversizedFrame_HugeAnnouncedLength(t *testing.T) {
	fx := newFixture(t)
	p := connect(t, fx.addr, "CP_HUGE")
	// FIN+text, masked, 64-bit length 0x7fff_ffff_ffff_ffff, mask, no payload.
	require.NoError(t, p.sendRaw([]byte{0x81, 0xff, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1, 2, 3, 4}))
	_, closed := p.awaitClosed(5 * time.Second)
	require.True(t, closed)
	fx.waitNoConns("CP_HUGE")
	fx.canary("CP_CANARY")
}

// A valid message sent as many small fragments is reassembled and answered; a
// message whose fragments add up past the limit is refused, however small each is.
func TestFragmentedFrames(t *testing.T) {
	fx := newFixture(t)

	t.Run("reassembled", func(t *testing.T) {
		p := connect(t, fx.addr, "CP_FRAG")
		msg := []byte(heartbeat)
		for i := 0; i < len(msg); i += 4 {
			end := min(i+4, len(msg))
			op := byte(opCont)
			if i == 0 {
				op = opText
			}
			p.send(end == len(msg), op, msg[i:end])
			if i == 8 { // a ping between fragments is allowed
				p.send(true, opPing, []byte("mid"))
			}
		}
		for {
			op, payload, err := p.readFrame(5 * time.Second)
			require.NoError(t, err)
			if op == opText {
				require.Contains(t, string(payload), `"hb-1"`)
				require.True(t, strings.HasPrefix(string(payload), "[3,"), string(payload))
				break
			}
		}
	})

	t.Run("fragments add up past the limit", func(t *testing.T) {
		p := connect(t, fx.addr, "CP_FRAGBIG")
		chunk := []byte(strings.Repeat("y", 1024))
		p.send(false, opText, []byte(`[2,"x","DataTransfer",{"vendorId":"v","data":"`))
		var code int
		for i := 0; i < 64 && code == 0; i++ {
			if err := p.sendRaw(frame(false, opCont, chunk)); err != nil {
				break
			}
			if c, closed := p.awaitClosed(5 * time.Millisecond); closed {
				code = c
			}
		}
		if code == 0 {
			code, _ = p.awaitClosed(5 * time.Second)
		}
		require.Equal(t, 1009, code)
		fx.waitNoConns("CP_FRAGBIG")
	})

	t.Run("continuation without a start, and a new message inside an unfinished one", func(t *testing.T) {
		p := connect(t, fx.addr, "CP_FRAGBAD")
		p.send(true, opCont, []byte("orphan"))
		_, closed := p.awaitClosed(5 * time.Second)
		require.True(t, closed)
		q := connect(t, fx.addr, "CP_FRAGBAD2")
		q.send(false, opText, []byte(`[2,"a","Heart`))
		q.send(true, opText, []byte(heartbeat)) // protocol violation: data frame before the FIN
		_, closed = q.awaitClosed(5 * time.Second)
		require.True(t, closed)
	})

	fx.waitNoConns("CP_FRAG", "CP_FRAGBIG", "CP_FRAGBAD", "CP_FRAGBAD2")
	fx.canary("CP_CANARY")
}

// Invalid UTF-8 in a text frame. RFC 6455 section 8.1 asks an endpoint to fail
// the connection (1007); the WebSocket library underneath does not check, and
// chargers in the field do send Latin-1, so the frame reaches the OCPP-J layer.
// What is required here: no panic, no wedged connection, and either the
// connection is failed with 1007 or the next valid CALL on it is answered.
func TestInvalidUTF8(t *testing.T) {
	fx := newFixture(t)
	for name, payload := range map[string][]byte{
		"lone continuation byte": []byte("[2,\"u\",\"Heartbeat\",{\"a\":\"\x80\"}]"),
		"overlong":               []byte("[2,\"u\",\"Heartbeat\",{\"a\":\"\xc0\xaf\"}]"),
		"surrogate":              []byte("[2,\"u\",\"Heartbeat\",{\"a\":\"\xed\xa0\x80\"}]"),
		"truncated":              []byte("[2,\"u\",\"Heartbeat\",{\"a\":\"\xe3\x81"),
		"in the message id":      []byte("[2,\"\xff\xfe\",\"Heartbeat\",{}]"),
		"in the action":          []byte("[2,\"u\",\"Heart\xffbeat\",{}]"),
	} {
		p := connect(t, fx.addr, "CP_UTF8")
		p.send(true, opText, payload)
		p.send(true, opText, []byte(`[2,"after","Heartbeat",{}]`))
		answered := false
		for !answered {
			op, got, err := p.readFrame(5 * time.Second)
			if err != nil {
				t.Fatalf("%s: connection neither answered nor was failed cleanly: %v", name, err)
			}
			switch op {
			case opClose:
				code := int(binary.BigEndian.Uint16(append(got, 0, 0)))
				require.Equal(t, 1007, code, name)
				_ = p.sendRaw(frame(true, opClose, got))
				answered = true
			case opText:
				answered = strings.Contains(string(got), `"after"`)
			}
		}
		p.reset()
		fx.waitNoConns("CP_UTF8")
	}
	fx.canary("CP_CANARY")
}

// A ping flood from a peer that never reads: the server keeps serving others.
func TestPingFlood(t *testing.T) {
	fx := newFixture(t)
	p := connect(t, fx.addr, "CP_PINGER")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ping := frame(true, opPing, []byte("0123456789abcdef"))
		deadline := time.Now().Add(1500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if p.sendRaw(ping) != nil {
				return
			}
		}
	}()
	for i := 0; i < 5; i++ {
		fx.canary("CP_CANARY_" + string(rune('A'+i)))
	}
	wg.Wait()
	p.reset()
	fx.waitNoConns("CP_PINGER")
}

// Resets at every stage: before the upgrade, during it, mid-frame and with a
// server call outstanding.
func TestAbruptReset(t *testing.T) {
	fx := newFixture(t)

	// before any byte is sent
	for i := 0; i < 20; i++ {
		nc := dialTCP(t, fx.addr)
		_ = nc.SetLinger(0)
		_ = nc.Close()
	}
	// in the middle of the upgrade request
	for i := 0; i < 20; i++ {
		nc := dialTCP(t, fx.addr)
		_, _ = nc.Write([]byte(upgradeRequest(fx.addr, "CP_RST")[:40]))
		_ = nc.SetLinger(0)
		_ = nc.Close()
	}
	// in the middle of a frame
	p := connect(t, fx.addr, "CP_RST_FRAME")
	whole := frame(true, opText, []byte(heartbeat))
	require.NoError(t, p.sendRaw(whole[:len(whole)/2]))
	p.reset()
	fx.waitNoConns("CP_RST_FRAME")

	// with a server-initiated call unanswered
	q := connect(t, fx.addr, "CP_RST_CALL")
	require.Eventually(t, func() bool { _, ok := fx.srv.Get("CP_RST_CALL"); return ok }, 3*time.Second, 10*time.Millisecond)
	conn, _ := fx.srv.Get("CP_RST_CALL")
	done := make(chan struct{})
	go func() {
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = csms.Call(ctx, conn, v16p.Reset, v16msg.ResetRequest{Type: v16msg.ResetRequestTypeSoft})
	}()
	_, _, err := q.readFrame(5 * time.Second) // the Reset call arrives
	require.NoError(t, err)
	q.reset()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("a call to a peer that was reset did not return")
	}
	fx.waitNoConns("CP_RST_CALL")
	fx.canary("CP_CANARY")
}

// Garbage mixed into a live conversation: the valid CALLs around it are answered.
func TestInterleavedGarbage(t *testing.T) {
	fx := newFixture(t)
	p := connect(t, fx.addr, "CP_MIXED")
	garbage := [][]byte{
		[]byte(`not json at all`),
		[]byte(`[`),
		[]byte(`{}`),
		[]byte(`[2,"g1"]`),
		[]byte(`[2,"g2","Heartbeat","not-an-object"]`),
		[]byte(`[99,"g3","X",{}]`),
		[]byte(`[3,"never-asked",{}]`),
		[]byte(`[4,"never-asked","GenericError","x",{}]`),
		[]byte(`[2,"g4","NoSuchAction",{}]`),
		[]byte(strings.Repeat("[", 4000)),
		{0, 1, 2, 3},
	}
	answers := 0
	for i, g := range garbage {
		p.send(true, opText, g)
		p.send(true, opBinary, g) // binary frames are not OCPP-J at all
		id := "ok-" + string(rune('a'+i))
		p.send(true, opText, []byte(`[2,"`+id+`","Heartbeat",{}]`))
		for {
			op, payload, err := p.readFrame(5 * time.Second)
			require.NoError(t, err, "connection lost after garbage %q", g)
			if op == opClose {
				t.Fatalf("server closed the connection after garbage %q: %q", g, payload)
			}
			if op == opText && strings.Contains(string(payload), `"`+id+`"`) {
				require.True(t, strings.HasPrefix(string(payload), "[3,"), string(payload))
				answers++
				break
			}
		}
	}
	require.Equal(t, len(garbage), answers)
	fx.canary("CP_CANARY")
}

// Many hostile connections at once, each ending differently, with a canary
// running throughout.
func TestHostileChurn(t *testing.T) {
	fx := newFixture(t)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				nc := dialTCP(t, fx.addr)
				p := &rawPeer{t: t, nc: nc}
				switch i % 4 {
				case 0:
					_ = p.sendRaw([]byte("GARBAGE\r\n\r\n"))
				case 1:
					_ = p.sendRaw([]byte(upgradeRequest(fx.addr, "CP_CHURN")[:60]))
				case 2:
					_ = p.sendRaw([]byte(upgradeRequest(fx.addr, "CP_CHURN_X")))
					time.Sleep(20 * time.Millisecond)
					_ = p.sendRaw(frame(true, opText, []byte("\xff\xfe")))
				case 3:
					_ = p.sendRaw([]byte(upgradeRequest(fx.addr, "CP_CHURN_Y")))
					time.Sleep(20 * time.Millisecond)
				}
				p.reset()
			}
		}()
	}
	for i := 0; i < 10; i++ {
		fx.canary("CP_CANARY_" + string(rune('A'+i)))
	}
	close(stop)
	wg.Wait()
	fx.waitNoConns("CP_CHURN", "CP_CHURN_X", "CP_CHURN_Y")
}
