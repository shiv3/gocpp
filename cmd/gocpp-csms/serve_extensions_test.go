package main

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestServeAllExtensions starts serve with every optional extension enabled
// and calls one control route of each. Overlapping http.ServeMux patterns of
// two extensions panic when they are registered, so a new overlap fails here.
func TestServeAllExtensions(t *testing.T) {
	cmd := csmsCommand("serve", "--ocpp-addr", "127.0.0.1:0", "--control-addr", "127.0.0.1:0", "--announce",
		"--drain-timeout", "1s", "--message-log", "--hosted", "--hosted-mode", "private",
		"--controller", "--controller-faults")
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr := &lockedBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})
	first := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		if sc.Scan() {
			first <- sc.Text()
		}
		close(first)
		_, _ = io.Copy(io.Discard, stdout)
	}()
	var m []string
	select {
	case l := <-first:
		if m = sentinelRE.FindStringSubmatch(l); m == nil {
			t.Fatalf("first line %q is not the ports sentinel; stderr:\n%s", l, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no ports sentinel; stderr:\n%s", stderr.String())
	}

	base := "http://127.0.0.1:" + m[2]
	call := func(method, path, body string, want int) {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s %s: status %d, want %d; body %s", method, path, resp.StatusCode, want, b)
		}
	}
	call(http.MethodGet, "/messages", "", http.StatusOK)                                              // message log
	call(http.MethodGet, "/instance", "", http.StatusOK)                                              // hosted
	call(http.MethodPost, "/registrations", `{"cpId":"CP1","displayName":"one"}`, http.StatusCreated) // hosted registry
	call(http.MethodGet, "/registrations", "", http.StatusOK)                                         // hosted registry
	call(http.MethodPost, "/registrations/CP1/password", "", http.StatusOK)                           // hosted registry
	call(http.MethodDelete, "/registrations/CP1", "", http.StatusOK)                                  // hosted registry
	call(http.MethodGet, "/chargepoints", "", http.StatusOK)                                          // controller
	call(http.MethodGet, "/faults", "", http.StatusOK)                                                // controller faults
	call(http.MethodGet, "/connections", "", http.StatusOK)                                           // built-in

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("exit: %v; stderr:\n%s", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no exit after SIGTERM")
	}
}
