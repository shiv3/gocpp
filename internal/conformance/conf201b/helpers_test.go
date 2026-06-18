package conf201b

import (
	"context"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	"github.com/stretchr/testify/require"
)

const subprotocol201 = "ocpp2.0.1"

type validationCase = conformance.ValidationCase

func newRegistry201(t *testing.T) *schema.Registry {
	t.Helper()

	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	return reg
}

func runValidation201(t *testing.T, action, kind string, cases []validationCase) {
	t.Helper()

	reg := newRegistry201(t)
	validator := conformance.MustValidator(t, reg, "2.0.1", action, kind)
	conformance.RunValidationTable(t, validator, cases)
}

func assertCSMSRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(subprotocol201))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func assertCPRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(subprotocol201))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func ptr[T any](v T) *T {
	return &v
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func certificateHashData() messages.CertificateHashDataType {
	return messages.CertificateHashDataType{
		HashAlgorithm:  "SHA256",
		IssuerNameHash: "hash00",
		IssuerKeyHash:  "hash01",
		SerialNumber:   "serial0",
	}
}

func ocspRequestData() messages.OCSPRequestDataType {
	return messages.OCSPRequestDataType{
		HashAlgorithm:  "SHA256",
		IssuerNameHash: "hash00",
		IssuerKeyHash:  "hash01",
		SerialNumber:   "serial0",
		ResponderURL:   "http://someUrl",
	}
}

func statusInfo(reasonCode string) *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: reasonCode}
}
