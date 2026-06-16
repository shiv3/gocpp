package transport_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
)

func TestFakeWS_ReadWrite(t *testing.T) {
	f := transport.NewFakeWS("ocpp1.6")
	ctx := context.Background()

	// Inject an inbound message; Read should return it.
	f.Inject([]byte("hello"))
	got, err := f.Read(ctx)
	if err != nil {
		t.Fatalf("FakeWS.Read() error = %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("FakeWS.Read() = %q, want %q", got, "hello")
	}

	// Write should be observable on the Sent channel.
	if err := f.Write(ctx, []byte("world")); err != nil {
		t.Fatalf("FakeWS.Write() error = %v", err)
	}
	if got := string(<-f.Sent()); got != "world" {
		t.Fatalf("FakeWS.Sent() = %q, want %q", got, "world")
	}

	if got := f.Subprotocol(); got != "ocpp1.6" {
		t.Fatalf("FakeWS.Subprotocol() = %q, want %q", got, "ocpp1.6")
	}

	if err := f.Ping(ctx); err != nil {
		t.Fatalf("FakeWS.Ping() error = %v", err)
	}
	if got := f.PingCount(); got != 1 {
		t.Fatalf("FakeWS.PingCount() = %d, want 1", got)
	}
}

func TestFakeWS_CloseUnblocksRead(t *testing.T) {
	f := transport.NewFakeWS("ocpp1.6")
	_ = f.Close(transport.StatusNormalClosure, "bye")
	_, err := f.Read(context.Background())
	if err == nil {
		t.Fatal("FakeWS.Read() error = nil, want non-nil")
	}
}

func TestFakeWS_PingFunc(t *testing.T) {
	want := errors.New("no pong")
	f := transport.NewFakeWS("ocpp1.6")
	f.SetPingFunc(func(context.Context) error {
		return want
	})

	err := f.Ping(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("FakeWS.Ping() error = %v, want %v", err, want)
	}
	if got := f.PingCount(); got != 1 {
		t.Fatalf("FakeWS.PingCount() = %d, want 1", got)
	}
}
