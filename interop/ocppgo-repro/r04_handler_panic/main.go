// r04: a panic in ONE application handler terminates the whole process.
//
// The parent starts the central system as a child process. Charge point
// "good" sends Heartbeat (answered). Charge point "bad" sends a Heartbeat
// whose handler panics (a nil dereference in application code). The parent
// then checks whether the server process is still alive and whether "good"
// is still served.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	if os.Getenv("R04_CHILD") == "1" {
		cs := h.StartCS(h.Opt{Handler: &h.Handler{Heartbeat: func(id string) (*core.HeartbeatConfirmation, error) {
			if id == "bad" {
				var p *core.HeartbeatConfirmation
				_ = p.CurrentTime.Time // application bug: nil pointer dereference
			}
			return nil, fmt.Errorf("x")
		}}})
		fmt.Println("PORT", cs.Port)
		select {}
	}
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "R04_CHILD=1")
	out, _ := child.StdoutPipe()
	errPipe, _ := child.StderrPipe()
	if err := child.Start(); err != nil {
		panic(err)
	}
	sc := bufio.NewScanner(out)
	port := 0
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[0] == "PORT" {
			port, _ = strconv.Atoi(f[1])
			break
		}
		fmt.Println(sc.Text())
	}
	stderr := make(chan string, 1)
	go func() {
		var b strings.Builder
		s := bufio.NewScanner(errPipe)
		for s.Scan() && b.Len() < 1200 {
			b.WriteString("    | " + s.Text() + "\n")
		}
		stderr <- b.String()
	}()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()

	fmt.Printf("library: %s\nserver process pid %d\n", h.Variant, child.Process.Pid)
	good := h.MustDial(port, "good")
	_ = good.Send(`[2,"g1","Heartbeat",{}]`)
	_, raw, _ := good.Read(3 * time.Second)
	fmt.Println("good: Heartbeat ->", raw)
	bad := h.MustDial(port, "bad")
	_ = bad.Send(`[2,"b1","Heartbeat",{}]`)
	_, raw, err := bad.Read(3 * time.Second)
	fmt.Printf("bad: Heartbeat (handler panics) -> %q err=%v\n", raw, err)
	died := false
	select {
	case err := <-exited:
		died = true
		fmt.Printf("server process EXITED: %v\nserver stderr (first lines):\n%s", err, <-stderr)
	case <-time.After(2 * time.Second):
		fmt.Println("server process still running")
	}
	_ = good.Send(`[2,"g2","Heartbeat",{}]`)
	_, raw, err = good.Read(3 * time.Second)
	fmt.Printf("good: Heartbeat after the panic -> %q err=%v\n", raw, err)
	if !died {
		_ = child.Process.Kill()
	}
	h.Result(died, "handler panic for one charge point; server process died=%v", died)
}
