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

func TestPublishFirmware21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "PublishFirmware", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.PublishFirmwareRequest{
				Checksum:  "abc123",
				Location:  "https://example.invalid/firmware.bin",
				RequestID: 1,
			},
			Valid: true,
		},
		{
			Name: "missing location",
			Message: map[string]any{
				"checksum":  "abc123",
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.PublishFirmwareRequest{
				Checksum:  longString(33),
				Location:  "https://example.invalid/firmware.bin",
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPublishFirmware21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "PublishFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.PublishFirmwareResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.PublishFirmwareResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo21(longString(21)),
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.PublishFirmwareResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPublishFirmware21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.PublishFirmware)
}
