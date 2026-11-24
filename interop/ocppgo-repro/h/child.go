package h

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// IsChild reports whether this process is the re-executed child for key.
func IsChild(key string) bool { return os.Getenv("REPRO_CHILD") == key }

// RunChild re-executes the program as child `key`, relays its stdout, waits
// up to d and returns whether it died by itself, with the head of its stderr.
func RunChild(key string, d time.Duration) (died bool, exitErr error, stderrHead string) {
	c := exec.Command(os.Args[0], os.Args[1:]...)
	c.Env = append(os.Environ(), "REPRO_CHILD="+key)
	c.Stdout = os.Stdout
	ep, _ := c.StderrPipe()
	if err := c.Start(); err != nil {
		panic(err)
	}
	head := make(chan string, 1)
	go func() {
		var b strings.Builder
		s := bufio.NewScanner(ep)
		s.Buffer(make([]byte, 1<<20), 1<<20)
		n := 0
		for s.Scan() {
			if n < 14 {
				b.WriteString("    | " + s.Text() + "\n")
			}
			n++
		}
		head <- b.String()
	}()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return true, err, <-head
	case <-time.After(d):
		_ = c.Process.Kill()
		<-done
		return false, nil, fmt.Sprintf("    (no crash within %v; child stopped by the parent)\n", d)
	}
}
