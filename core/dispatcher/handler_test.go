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
	defer c.Close(nil)

	f.Inject([]byte(`[2,"h1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"h1",{"currentTime":"2026-06-15T00:00:00Z"}]`, string(sent))
}

func TestConn_UnknownActionReturnsNotImplemented(t *testing.T) {
	defer goleak.VerifyNone(t)
	f := transport.NewFakeWS("ocpp1.6")
	c := NewConn("CP_1", f, DefaultConfig(), NewHandlerRegistry())
	c.Start(context.Background())
	defer c.Close(nil)

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
	defer c.Close(nil)

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
	defer c.Close(nil)

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
	defer c.Close(nil)

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
		return errors.New("schema nope")
	}
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	defer c.Close(nil)

	f.Inject([]byte(`[2,"tol1","Heartbeat",{}]`))

	sent := <-f.Sent()
	require.JSONEq(t, `[3,"tol1",{"ok":true}]`, string(sent))
	require.Equal(t, 1, validatorCalls)
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
	defer c.Close(nil)

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

type schemaFailureMetrics struct {
	schemaFailures int
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
