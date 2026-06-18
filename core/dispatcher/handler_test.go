package dispatcher

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestConn_HandlerRespondsCallResult(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"h1",{"currentTime":"2026-06-15T00:00:00Z"}]`, string(sent))
}

func TestConn_UnknownActionReturnsNotImplemented(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"x1","Nonexistent",{}]`))

	sent := <-f.Sent()
	require.Contains(t, string(sent), "NotImplemented")
	require.Contains(t, string(sent), `"x1"`)
}

func TestConn_HandlerPanicReturnsInternalErrorAndSurvives(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Boom", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		panic("handler blew up")
	})
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	// A panicking handler must not crash the process; it replies InternalError.
	f.Inject([]byte(`[2,"b1","Boom",{}]`))
	sent := <-f.Sent()
	require.Contains(t, string(sent), "InternalError")
	require.Contains(t, string(sent), `"b1"`)

	// The connection must still serve subsequent requests.
	f.Inject([]byte(`[2,"h2","Heartbeat",{}]`))
	sent = <-f.Sent()
	require.JSONEq(t, `[3,"h2",{"currentTime":"2026-06-15T00:00:00Z"}]`, string(sent))
}

func TestConn_HandlerErrorReturnsCallError(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Authorize", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return nil, ocppj.NewCallError(ocppj.ErrorCodePropertyConstraintViolation, "bad idTag", nil)
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"a1","Authorize",{}]`))
	sent := <-f.Sent()
	require.Contains(t, string(sent), "PropertyConstraintViolation")
}

func TestConn_SchemaModeOffSkipsValidation(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	})
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeOff
	validatorCalls := 0
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		validatorCalls++
		return errors.New("schema nope")
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"off1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"off1",{"ok":true}]`, string(sent))
	require.Zero(t, validatorCalls)
}

func TestConn_SchemaModeTolerantLogsAndContinues(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	})
	var logs bytes.Buffer
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeTolerant
	validatorCalls := 0
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		validatorCalls++
		if kind != "request" {
			return nil
		}
		return errors.New("schema nope")
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"tol1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"tol1",{"ok":true}]`, string(sent))
	require.Equal(t, 2, validatorCalls)
	require.Equal(t, 1, metrics.schemaFailures)
	require.Equal(t, "1.6", metrics.version)
	require.Equal(t, "Heartbeat", metrics.action)
	require.Equal(t, "request", metrics.kind)
	logText := logs.String()
	require.Contains(t, logText, "schema validation failed")
	require.Contains(t, logText, "version=1.6")
	require.Contains(t, logText, "action=Heartbeat")
	require.Contains(t, logText, "kind=request")
	require.Contains(t, logText, "schema nope")
}

func TestConn_SchemaModeStrictRejectsFormationViolation(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	handlerCalled := make(chan struct{}, 1)
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		handlerCalled <- struct{}{}
		return []byte(`{"ok":true}`), nil
	})
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		return errors.New("schema nope")
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"strict1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[4,"strict1","FormationViolation","schema nope",{}]`, string(sent))
	require.Equal(t, 1, metrics.schemaFailures)
	select {
	case <-handlerCalled:
		t.Fatal("handler was called after strict schema failure")
	default:
	}
}

func TestConn_StrictSchemaRejectsInvalidHandlerResponse(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"bad":true}`), nil
	})
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeStrict
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		if kind == "response" {
			return errors.New("schema nope")
		}
		return nil
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"resp1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[4,"resp1","InternalError","schema nope",{}]`, string(sent))
	require.Equal(t, 1, metrics.schemaFailures)
	require.Equal(t, "response", metrics.kind)
}

func TestConn_TolerantSchemaLogsInvalidHandlerResponseAndContinues(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"ok":true}`), nil
	})
	var logs bytes.Buffer
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeTolerant
	cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
		if kind == "response" {
			return errors.New("schema nope")
		}
		return nil
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"resp2","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"resp2",{"ok":true}]`, string(sent))
	require.Equal(t, 1, metrics.schemaFailures)
	require.Equal(t, "response", metrics.kind)
	require.Contains(t, logs.String(), "kind=response")
}

func TestConn_LenientDispatchesNormalizedPayload(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	gotPayload := make(chan []byte, 1)
	reg.Register("BootNotification", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		gotPayload <- append([]byte(nil), payload...)
		return []byte(`{}`), nil
	})
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeLenient
	cfg.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
		require.Equal(t, ocppj.V16, version)
		require.Equal(t, "BootNotification", action)
		if kind == "request" {
			return []byte(`{"status":"Accepted"}`), []string{"enum"}, nil
		}
		return payload, nil, nil
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"len1","BootNotification",{"status":"accepted"}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"len1",{}]`, string(sent))
	select {
	case got := <-gotPayload:
		require.JSONEq(t, `{"status":"Accepted"}`, string(got))
	default:
		t.Fatal("handler was not called")
	}
	require.Zero(t, metrics.schemaFailures)
	require.Equal(t, []string{"enum"}, metrics.softKeywords)
	require.Equal(t, "1.6", metrics.version)
	require.Equal(t, "BootNotification", metrics.action)
	require.Equal(t, "request", metrics.kind)
}

func TestConn_LenientHardFailureRejected(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	handlerCalled := make(chan struct{}, 1)
	reg.Register("BootNotification", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		handlerCalled <- struct{}{}
		return []byte(`{}`), nil
	})
	metrics := &schemaFailureMetrics{}
	cfg := DefaultConfig()
	cfg.Metrics = metrics
	cfg.SchemaMode = SchemaModeLenient
	cfg.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
		return nil, nil, errors.New("type mismatch")
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"len2","BootNotification",{"x":1}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[4,"len2","FormationViolation","type mismatch",{}]`, string(sent))
	require.Equal(t, 1, metrics.schemaFailures)
	select {
	case <-handlerCalled:
		t.Fatal("handler was called after lenient hard schema failure")
	default:
	}
}

func TestConn_LenientSendsNormalizedResponse(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("BootNotification", func(ctx context.Context, c *Conn, payload []byte) ([]byte, error) {
		return []byte(`{"status":"accepted"}`), nil
	})
	cfg := DefaultConfig()
	cfg.SchemaMode = SchemaModeLenient
	cfg.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
		if kind == "response" {
			return []byte(`{"status":"Accepted"}`), []string{"enum"}, nil
		}
		return payload, nil, nil
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[2,"len3","BootNotification",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"len3",{"status":"Accepted"}]`, string(sent))
}

type schemaFailureMetrics struct {
	schemaFailures int
	softKeywords   []string
	version        string
	action         string
	kind           string
}

func (m *schemaFailureMetrics) ConnectionOpened()                                   {}
func (m *schemaFailureMetrics) ConnectionClosed()                                   {}
func (m *schemaFailureMetrics) CallStarted(string, string)                          {}
func (m *schemaFailureMetrics) CallCompleted(string, string, time.Duration, string) {}
func (m *schemaFailureMetrics) PendingDelta(int)                                    {}
func (m *schemaFailureMetrics) SchemaValidationFailure(version, action, kind string) {
	m.schemaFailures++
	m.version = version
	m.action = action
	m.kind = kind
}
func (m *schemaFailureMetrics) SchemaSoftViolation(version, action, kind, keyword string) {
	m.softKeywords = append(m.softKeywords, keyword)
	m.version = version
	m.action = action
	m.kind = kind
}

func TestConn_SendNoReply(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	got := make(chan []byte, 1)
	reg.RegisterSend("NotifyPeriodicEventStream", func(_ context.Context, _ *Conn, payload []byte) error {
		got <- append([]byte(nil), payload...)
		return nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[6,"m1","NotifyPeriodicEventStream",{"id":1}]`))

	select {
	case p := <-got:
		require.JSONEq(t, `{"id":1}`, string(p))
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called")
	}
	// SEND must never produce a reply.
	select {
	case b := <-f.Sent():
		t.Fatalf("SEND must not reply, got %s", b)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestConn_SendHandlerPanicRecoveredAndSurvives(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	ok := make(chan struct{}, 1)
	reg.RegisterSend("PanicSend", func(_ context.Context, _ *Conn, _ []byte) error {
		panic("boom")
	})
	reg.RegisterSend("GoodSend", func(_ context.Context, _ *Conn, _ []byte) error {
		ok <- struct{}{}
		return nil
	})
	c := NewConn("CP_1", f, DefaultConfig(), reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	// A panicking SEND handler must not crash the process or reply.
	f.Inject([]byte(`[6,"m1","PanicSend",{}]`))
	// The connection must survive: a subsequent SEND is still dispatched.
	f.Inject([]byte(`[6,"m2","GoodSend",{}]`))

	select {
	case <-ok:
	case <-time.After(2 * time.Second):
		t.Fatal("connection did not survive a panicking SEND handler")
	}
	select {
	case b := <-f.Sent():
		t.Fatalf("SEND must not reply, got %s", b)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestConn_SendMissingHandlerNoReply(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()

	f.Inject([]byte(`[6,"m2","Unknown",{}]`))

	select {
	case b := <-f.Sent():
		t.Fatalf("missing SEND handler must not reply, got %s", b)
	case <-time.After(200 * time.Millisecond):
	}
}
