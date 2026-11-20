package dispatcher

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

// echoRig starts a connection whose "Heartbeat" handler answers {} and returns
// the fake socket. Replies come back on f.Sent().
func echoRig(t *testing.T, sub string, mutate func(*Config)) *transport.FakeWS {
	t.Helper()
	f := transport.NewFakeWS(sub)
	cfg := DefaultConfig()
	if mutate != nil {
		mutate(&cfg)
	}
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) { return []byte(`{}`), nil })
	c := NewConn("CP_1", f, cfg, reg)
	c.Start(context.Background())
	t.Cleanup(func() { _ = c.Close(nil) })
	return f
}

func recvArr(t *testing.T, f *transport.FakeWS) []any {
	t.Helper()
	select {
	case b := <-f.Sent():
		var arr []any
		require.NoError(t, json.Unmarshal(b, &arr))
		return arr
	case <-time.After(2 * time.Second):
		t.Fatal("no frame sent")
		return nil
	}
}

func requireSilent(t *testing.T, f *transport.FakeWS) {
	t.Helper()
	select {
	case b := <-f.Sent():
		t.Fatalf("unexpected frame %s", b)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestMessageIDPolicy(t *testing.T) {
	long := strings.Repeat("x", 37)
	ok36 := strings.Repeat("y", 36)
	strict := func(c *Config) { c.SchemaMode = SchemaModeStrict }
	lenient := func(c *Config) { c.SchemaMode = SchemaModeLenient }
	tolerant := func(c *Config) { c.SchemaMode = SchemaModeTolerant }

	t.Run("strict answers a long id and echoes it", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", strict)
		f.Inject([]byte(`[2,"` + long + `","Heartbeat",{}]`))
		arr := recvArr(t, f)
		require.Equal(t, []any{float64(4), long, "FormationViolation"}, arr[:3])
	})
	t.Run("strict answers a long id with FormatViolation on 2.x", func(t *testing.T) {
		f := echoRig(t, "ocpp2.0.1", strict)
		f.Inject([]byte(`[2,"` + long + `","Heartbeat",{}]`))
		require.Equal(t, "FormatViolation", recvArr(t, f)[2])
	})
	t.Run("strict drops an empty id", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", strict)
		f.Inject([]byte(`[2,"","Heartbeat",{}]`))
		requireSilent(t, f)
	})
	t.Run("strict accepts 36 characters", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", strict)
		f.Inject([]byte(`[2,"` + ok36 + `","Heartbeat",{}]`))
		require.Equal(t, float64(3), recvArr(t, f)[0])
	})
	t.Run("the limit counts characters, not bytes", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", strict)
		f.Inject([]byte(`[2,"` + strings.Repeat("é", 36) + `","Heartbeat",{}]`))
		require.Equal(t, float64(3), recvArr(t, f)[0])
	})
	for name, mut := range map[string]func(*Config){"lenient": lenient, "tolerant": tolerant, "off": nil} {
		t.Run(name+" accepts long and empty ids", func(t *testing.T) {
			f := echoRig(t, "ocpp1.6", mut)
			f.Inject([]byte(`[2,"` + long + `","Heartbeat",{}]`))
			require.Equal(t, long, recvArr(t, f)[1])
			f.Inject([]byte(`[2,"","Heartbeat",{}]`))
			require.Equal(t, "", recvArr(t, f)[1])
		})
	}
	t.Run("explicit Reject overrides lenient", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", func(c *Config) { lenient(c); c.MessageIDPolicy = MessageIDReject })
		f.Inject([]byte(`[2,"` + long + `","Heartbeat",{}]`))
		require.Equal(t, "FormationViolation", recvArr(t, f)[2])
	})
	t.Run("explicit Accept overrides strict", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", func(c *Config) { strict(c); c.MessageIDPolicy = MessageIDAccept })
		f.Inject([]byte(`[2,"` + long + `","Heartbeat",{}]`))
		require.Equal(t, float64(3), recvArr(t, f)[0])
	})
	t.Run("Drop is silent for both", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", func(c *Config) { c.MessageIDPolicy = MessageIDDrop })
		f.Inject([]byte(`[2,"` + long + `","Heartbeat",{}]`))
		f.Inject([]byte(`[2,"","Heartbeat",{}]`))
		requireSilent(t, f)
		f.Inject([]byte(`[2,"ok","Heartbeat",{}]`))
		require.Equal(t, "ok", recvArr(t, f)[1])
	})
}

func TestOCPPGoErrorCodeMapper(t *testing.T) {
	mut := func(c *Config) { c.ErrorCodeMapper = OCPPGoErrorCodeMapper() }

	t.Run("defaults without a mapper", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", nil)
		f.Inject([]byte(`[2,"a","Foo",{}]`))
		require.Equal(t, "NotImplemented", recvArr(t, f)[2])
		f.Inject([]byte(`[9,"b","Foo",{}]`))
		requireSilent(t, f)
	})
	t.Run("unknown action is NotSupported", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", mut)
		f.Inject([]byte(`[2,"a","Foo",{}]`))
		require.Equal(t, "NotSupported", recvArr(t, f)[2])
	})
	t.Run("unknown message type is answered on 1.6", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", mut)
		f.Inject([]byte(`[9,"b","Foo",{}]`))
		arr := recvArr(t, f)
		require.Equal(t, []any{float64(4), "b", "MessageTypeNotSupported"}, arr[:3])
	})
	t.Run("type 6 on 1.6 is answered like other unknown types", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", mut)
		f.Inject([]byte(`[6,"s","Heartbeat",{}]`))
		require.Equal(t, []any{float64(4), "s", "MessageTypeNotSupported"}, recvArr(t, f)[:3])
	})
	t.Run("malformed CALLs", func(t *testing.T) {
		f := echoRig(t, "ocpp1.6", mut)
		f.Inject([]byte(`[2,"a"]`)) // too short: ocpp-go stays silent
		requireSilent(t, f)
		f.Inject([]byte(`[2,"b",5,{}]`)) // action not a string: silent
		requireSilent(t, f)
		f.Inject([]byte(`[2,"c","Heartbeat"]`)) // 3 elements: answered
		require.Equal(t, "FormationViolation", recvArr(t, f)[2])
		f.Inject([]byte(`[2,"d","Heartbeat",{},1]`)) // 5 elements: answered
		require.Equal(t, "FormationViolation", recvArr(t, f)[2])
	})
	t.Run("custom mapper sees the source and can silence", func(t *testing.T) {
		seen := make(chan ErrorSource, 1)
		f := echoRig(t, "ocpp2.0.1", func(c *Config) {
			c.ErrorCodeMapper = func(ctx ErrorContext) (string, bool) {
				seen <- ctx.Source
				require.Equal(t, "Foo", ctx.Action)
				require.Equal(t, "NotImplemented", ctx.WireCode)
				return "", false
			}
		})
		f.Inject([]byte(`[2,"a","Foo",{}]`))
		requireSilent(t, f)
		require.Equal(t, ErrorSourceUnknownAction, <-seen)
	})
	t.Run("handler-returned errors are never mapped", func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		cfg := DefaultConfig()
		cfg.ErrorCodeMapper = func(ErrorContext) (string, bool) { return "GenericError", true }
		reg := NewHandlerRegistry()
		reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
			return nil, ocppj.NewCallError(ocppj.ErrorCodeNotImplemented, "mine", nil)
		})
		c := NewConn("CP_1", f, cfg, reg)
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()
		f.Inject([]byte(`[2,"a","Heartbeat",{}]`))
		require.Equal(t, "NotImplemented", recvArr(t, f)[2])
	})
	t.Run("decode failures map to FormationViolation", func(t *testing.T) {
		f := transport.NewFakeWS("ocpp1.6")
		cfg := DefaultConfig()
		cfg.ErrorCodeMapper = OCPPGoErrorCodeMapper()
		reg := NewHandlerRegistry()
		reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
			var v struct{ N int }
			if err := json.Unmarshal([]byte(`{"N":"x"}`), &v); err != nil {
				return nil, NewDecodeError(err)
			}
			return []byte(`{}`), nil
		})
		c := NewConn("CP_1", f, cfg, reg)
		c.Start(context.Background())
		defer func() { _ = c.Close(nil) }()
		f.Inject([]byte(`[2,"a","Heartbeat",{}]`))
		require.Equal(t, "FormationViolation", recvArr(t, f)[2])
	})
}
