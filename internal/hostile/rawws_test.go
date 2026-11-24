package hostile

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// rawPeer is a hand-rolled WebSocket client: it can say things a conforming
// library never would (fragment storms, bad UTF-8, half frames, resets).
type rawPeer struct {
	t  *testing.T
	nc *net.TCPConn
	br *bufio.Reader
}

const (
	opCont   = 0x0
	opText   = 0x1
	opBinary = 0x2
	opClose  = 0x8
	opPing   = 0x9
	opPong   = 0xA
)

func dialTCP(t *testing.T, addr string) *net.TCPConn {
	t.Helper()
	nc, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	return nc.(*net.TCPConn)
}

// upgradeRequest is a well-formed WebSocket upgrade request for id.
func upgradeRequest(addr, id string) string {
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	return fmt.Sprintf("GET /ocpp/%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: ocpp1.6\r\n\r\n",
		id, addr, b64(key))
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// connect performs the upgrade and returns a peer that is ready to send frames.
func connect(t *testing.T, addr, id string) *rawPeer {
	t.Helper()
	nc := dialTCP(t, addr)
	p := &rawPeer{t: t, nc: nc, br: bufio.NewReader(nc)}
	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(nc, upgradeRequest(addr, id)); err != nil {
		t.Fatalf("write upgrade: %v", err)
	}
	status, err := p.br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("upgrade refused: %q %v", status, err)
	}
	for {
		line, err := p.br.ReadString('\n')
		if err != nil {
			t.Fatalf("read upgrade response: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}
	_ = nc.SetDeadline(time.Time{})
	t.Cleanup(func() { _ = nc.Close() })
	return p
}

// frame encodes one masked client frame.
func frame(fin bool, op byte, payload []byte) []byte {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	out := []byte{b0}
	switch n := len(payload); {
	case n < 126:
		out = append(out, 0x80|byte(n))
	case n < 1<<16:
		out = append(out, 0x80|126, 0, 0)
		binary.BigEndian.PutUint16(out[len(out)-2:], uint16(n))
	default:
		out = append(out, 0x80|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(out[len(out)-8:], uint64(n))
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	out = append(out, mask[:]...)
	for i, c := range payload {
		out = append(out, c^mask[i%4])
	}
	return out
}

func (p *rawPeer) send(fin bool, op byte, payload []byte) {
	p.t.Helper()
	_ = p.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := p.nc.Write(frame(fin, op, payload)); err != nil {
		p.t.Fatalf("write frame: %v", err)
	}
}

// sendRaw writes bytes as they are, ignoring write errors (the server may
// already have hung up).
func (p *rawPeer) sendRaw(b []byte) error {
	_ = p.nc.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := p.nc.Write(b)
	return err
}

// readFrame reads one server frame (servers do not mask).
func (p *rawPeer) readFrame(timeout time.Duration) (op byte, payload []byte, err error) {
	_ = p.nc.SetReadDeadline(time.Now().Add(timeout))
	var h [2]byte
	if _, err = io.ReadFull(p.br, h[:]); err != nil {
		return 0, nil, err
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var l [2]byte
		if _, err = io.ReadFull(p.br, l[:]); err != nil {
			return 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(l[:]))
	case 127:
		var l [8]byte
		if _, err = io.ReadFull(p.br, l[:]); err != nil {
			return 0, nil, err
		}
		n = binary.BigEndian.Uint64(l[:])
	}
	payload = make([]byte, n)
	_, err = io.ReadFull(p.br, payload)
	return h[0] & 0x0f, payload, err
}

// awaitClosed reads until the server closes the connection (close frame or EOF)
// and returns the close status code, or 0 when the TCP connection just ended.
func (p *rawPeer) awaitClosed(timeout time.Duration) (code int, ok bool) {
	deadline := time.Now().Add(timeout)
	for time.Until(deadline) > 0 {
		op, payload, err := p.readFrame(time.Until(deadline))
		if err != nil {
			if ne, isNet := err.(net.Error); isNet && ne.Timeout() {
				return 0, false
			}
			return 0, true
		}
		if op == opClose {
			// Complete the close handshake like a conforming peer would.
			_ = p.sendRaw(frame(true, opClose, payload))
			_ = p.nc.Close()
			if len(payload) >= 2 {
				return int(binary.BigEndian.Uint16(payload)), true
			}
			return 1005, true
		}
	}
	return 0, false
}

// reset aborts the connection with a TCP RST.
func (p *rawPeer) reset() {
	_ = p.nc.SetLinger(0)
	_ = p.nc.Close()
}
