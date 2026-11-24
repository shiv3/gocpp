package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9100", "")
	path := flag.String("path", "/", "")
	n := flag.Int("n", 4000, "")
	idle := flag.Duration("idle", 5*time.Second, "")
	after := flag.Duration("after", 8*time.Second, "")
	flag.Parse()
	conns := make([]net.Conn, 0, *n)
	for i := 0; i < *n; i++ {
		c, err := net.Dial("tcp", *addr)
		if err != nil {
			fmt.Println("dial", i, err)
			os.Exit(1)
		}
		fmt.Fprintf(c, "GET %scp%05d HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: ocpp1.6\r\n\r\n", *path, i)
		br := bufio.NewReader(c)
		resp, err := http.ReadResponse(br, nil)
		if err != nil || resp.StatusCode != 101 {
			fmt.Println("handshake", i, err, resp)
			os.Exit(1)
		}
		conns = append(conns, c)
	}
	fmt.Println("connected", len(conns))
	time.Sleep(*idle)
	fmt.Println("PHASE idle")
	os.Stdout.Sync()
	time.Sleep(*after)
	msg := []byte(`[2,"1","Heartbeat",{}]`)
	fr := append([]byte{0x81, 0x80 | byte(len(msg)), 0, 0, 0, 0}, msg...)
	for _, c := range conns {
		c.Write(fr)
	}
	time.Sleep(3 * time.Second)
	ok := 0
	buf := make([]byte, 256)
	for _, c := range conns {
		c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if n, _ := c.Read(buf); n > 10 {
			ok++
		}
	}
	fmt.Println("replies", ok)
	fmt.Println("PHASE hb")
	time.Sleep(*after)
}
