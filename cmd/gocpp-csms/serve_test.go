package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
)

// TestServeProcess runs "gocpp-csms serve" as a process: ports sentinel with
// --announce, recorder lines with --record, exit 0 on SIGTERM.
func TestServeProcess(t *testing.T) {
	cmd := csmsCommand("serve", "--ocpp-addr", "127.0.0.1:0", "--control-addr", "127.0.0.1:0", "--announce", "--record", "--drain-timeout", "1s")
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, "GOCPP_CSMS_LOG_FORMAT=text")
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
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	next := func() string {
		t.Helper()
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stdout closed; stderr:\n%s", stderr.String())
			}
			return l
		case <-time.After(10 * time.Second):
			t.Fatalf("no stdout line; stderr:\n%s", stderr.String())
		}
		return ""
	}
	m := sentinelRE.FindStringSubmatch(next())
	if m == nil {
		t.Fatal("first line is not the ports sentinel")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, sub := range []string{"ocpp1.6", "ocpp2.0.1"} {
		c := cp.NewClient("CP-"+sub, fmt.Sprintf("ws://127.0.0.1:%s/ocpp/CP-%s", m[1], sub), cp.WithSubProtocols(sub))
		if err := c.Connect(ctx); err != nil {
			t.Fatalf("%s: %v", sub, err)
		}
		defer c.Close()
		if _, err := cp.CallRaw(ctx, c, "Heartbeat", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	for i, sub := range []string{"ocpp1.6", "ocpp2.0.1"} {
		want := fmt.Sprintf(`{"seq":%d,"cpId":"CP-%s","action":"Heartbeat","payload":{}}`, i+1, sub)
		if got := next(); got != want {
			t.Fatalf("recorder line %q, want %q", got, want)
		}
	}
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

func TestServeUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"serve", "--nope"},
		{"serve", "--versions", "1.5"},
		{"serve", "extra"},
	} {
		cmd := csmsCommand(args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("%v: err %v, want exit status 2; stderr %s", args, err, stderr.String())
		}
	}
}
