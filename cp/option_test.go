package cp

import (
	"crypto/tls"
	"encoding/base64"
	"net/http"
	"testing"
	"testing/fstest"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/stretchr/testify/require"
)

func TestSchemaOptions_LastWinsAndWireDispatcher(t *testing.T) {
	reg := testSchemaRegistry(t, "ChangeConfiguration")

	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithStrictSchema(true),
		WithTolerantSchema(),
	)
	require.Equal(t, dispatcher.SchemaModeTolerant, client.cfg.dispatcher.SchemaMode)
	require.NotNil(t, client.cfg.dispatcher.SchemaValidate)
	require.Error(t, client.cfg.dispatcher.SchemaValidate("1.6", "ChangeConfiguration", "request", []byte(`{}`)))

	client = NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithTolerantSchema(),
		WithStrictSchema(true),
	)
	require.Equal(t, dispatcher.SchemaModeStrict, client.cfg.dispatcher.SchemaMode)
	require.NotNil(t, client.cfg.dispatcher.SchemaValidate)

	client = NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSchemaRegistry(reg),
		WithTolerantSchema(),
		WithStrictSchema(false),
	)
	require.Equal(t, dispatcher.SchemaModeOff, client.cfg.dispatcher.SchemaMode)
	require.Nil(t, client.cfg.dispatcher.SchemaValidate)
}

func TestWebSocketPingOptionsWireDispatcher(t *testing.T) {
	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithWebSocketPingInterval(15*time.Second),
		WithWebSocketPongWait(3*time.Second),
	)

	require.Equal(t, 15*time.Second, client.cfg.dispatcher.PingInterval)
	require.Equal(t, 3*time.Second, client.cfg.dispatcher.PongWait)
}

func TestWithSerializedCalls(t *testing.T) {
	client := NewClient(
		"CP_1",
		"ws://example.invalid/ocpp/CP_1",
		WithSerializedCalls(),
	)

	require.True(t, client.cfg.dispatcher.SerializeOutboundCalls)
}

func TestClientConfigDialOptions(t *testing.T) {
	tlsCfg := &tls.Config{ServerName: "csms.example"}
	cfg := defaultClientConfig()
	WithSubProtocols("ocpp2.0.1", "ocpp1.6").apply(&cfg)
	WithHTTPHeader("X-Trace-ID", "trace-1").apply(&cfg)
	WithHTTPHeader("X-Trace-ID", "trace-2").apply(&cfg)
	WithHTTPHeader("X-Client", "cp").apply(&cfg)
	WithBasicAuth("alice", "secret").apply(&cfg)
	WithTLSConfig(tlsCfg).apply(&cfg)

	opts := cfg.dialOptions()

	require.Equal(t, []string{"ocpp2.0.1", "ocpp1.6"}, opts.Subprotocols)
	require.Equal(t, []string{"trace-1", "trace-2"}, opts.HTTPHeader.Values("X-Trace-ID"))
	require.Equal(t, "cp", opts.HTTPHeader.Get("X-Client"))
	require.Equal(t,
		"Basic "+base64.StdEncoding.EncodeToString([]byte("alice:secret")),
		opts.HTTPHeader.Get("Authorization"),
	)
	require.NotNil(t, opts.HTTPClient)
	require.Same(t, tlsCfg, opts.HTTPClient.Transport.(*http.Transport).TLSClientConfig)
}

func testSchemaRegistry(t *testing.T, action string) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	err := reg.Register("1.6", action, "request", fstest.MapFS{
		"request.json": {
			Data: []byte(`{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`),
		},
	}, "request.json")
	require.NoError(t, err)
	return reg
}
