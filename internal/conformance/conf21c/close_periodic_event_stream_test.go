package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestClosePeriodicEventStream21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "ClosePeriodicEventStream", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClosePeriodicEventStreamRequest{
				ID: 1,
			},
			Valid: true,
		},
		{
			Name:    "missing id",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.ClosePeriodicEventStreamRequest{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
				ID:         1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClosePeriodicEventStream21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "ClosePeriodicEventStream", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.ClosePeriodicEventStreamResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.ClosePeriodicEventStreamResponse{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClosePeriodicEventStream21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.ClosePeriodicEventStream)
}
