// Package conformance provides shared helpers for porting message-level OCPP
// conformance tests.
package conformance

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

// ValidationCase is one named JSON Schema validation scenario.
type ValidationCase struct {
	Name    string
	Message any
	Valid   bool
}

// RunValidationTable marshals and validates each case as a named subtest.
func RunValidationTable(t *testing.T, v *schema.Validator, cases []ValidationCase) {
	t.Helper()

	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Helper()
			require.NotEmpty(t, c.Name, "validation case must have a descriptive Name")

			raw, err := json.Marshal(c.Message)
			require.NoError(t, err, "marshal %q", c.Name)

			err = v.Validate(raw)
			require.Equalf(t, c.Valid, err == nil, "%s validation mismatch: %v; payload=%s", c.Name, err, raw)
		})
	}
}

// MustValidator returns the registered validator or fails the test immediately.
func MustValidator(t *testing.T, reg *schema.Registry, version, action, kind string) *schema.Validator {
	t.Helper()

	v, ok := reg.Lookup(version, action, kind)
	require.Truef(t, ok, "missing validator for %s/%s/%s", version, action, kind)
	return v
}

// RoundTripCSMS starts a CSMS, connects a CP, sends one CP-originated request,
// and returns the typed response.
func RoundTripCSMS[Req, Resp any](
	t *testing.T,
	subprotocol string,
	reg *schema.Registry,
	register func(*csms.Server),
	msg ocppj.Message[Req, Resp],
	req Req,
) (Resp, error) {
	t.Helper()

	serverOpts := []csms.Option{csms.WithSubProtocols(subprotocol)}
	clientOpts := []cp.Option{cp.WithSubProtocols(subprotocol)}
	if reg != nil {
		serverOpts = append(serverOpts, csms.WithSchemaRegistry(reg), csms.WithStrictSchema(true))
		clientOpts = append(clientOpts, cp.WithSchemaRegistry(reg), cp.WithStrictSchema(true))
	}

	srv := csms.NewServer(serverOpts...)
	if register != nil {
		register(srv)
	}
	defer srv.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, clientOpts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var zero Resp
	if err := client.Connect(ctx); err != nil {
		return zero, err
	}
	defer client.Close()

	return cp.Call(ctx, client, msg, req)
}

// RoundTripCP starts a CSMS, connects a CP (which registers handlers via
// register), waits for the connection to appear, then sends one CSMS-originated
// request to the charge point and returns the typed response. Use this for
// SentByCSMS messages (the mirror of RoundTripCSMS).
func RoundTripCP[Req, Resp any](
	t *testing.T,
	subprotocol string,
	reg *schema.Registry,
	register func(*cp.Client),
	msg ocppj.Message[Req, Resp],
	req Req,
) (Resp, error) {
	t.Helper()

	serverOpts := []csms.Option{csms.WithSubProtocols(subprotocol)}
	clientOpts := []cp.Option{cp.WithSubProtocols(subprotocol)}
	if reg != nil {
		serverOpts = append(serverOpts, csms.WithSchemaRegistry(reg), csms.WithStrictSchema(true))
		clientOpts = append(clientOpts, cp.WithSchemaRegistry(reg), cp.WithStrictSchema(true))
	}

	srv := csms.NewServer(serverOpts...)
	defer srv.Close()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, clientOpts...)
	if register != nil {
		register(client)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var zero Resp
	if err := client.Connect(ctx); err != nil {
		return zero, err
	}
	defer client.Close()

	for {
		if conn, ok := srv.Get("CP_1"); ok {
			return csms.Call(ctx, conn, msg, req)
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
