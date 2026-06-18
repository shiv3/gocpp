package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestPublishFirmware201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "PublishFirmware", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.PublishFirmwareRequest{
				Location:      "https://someurl",
				Retries:       ptr(int32(5)),
				Checksum:      "deadbeef",
				RequestID:     42,
				RetryInterval: ptr(int32(300)),
			},
			Valid: true,
		},
		{
			Name: "valid without retryInterval",
			Message: messages.PublishFirmwareRequest{
				Location:  "http://someurl",
				Retries:   ptr(int32(5)),
				Checksum:  "deadbeef",
				RequestID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid without retries",
			Message: messages.PublishFirmwareRequest{
				Location:  "http://someurl",
				Checksum:  "deadbeef",
				RequestID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.PublishFirmwareRequest{
				Location: "http://someurl",
				Checksum: "deadbeef",
			},
			Valid: true,
		},
		{
			Name: "invalid missing checksum",
			Message: map[string]any{
				"location": "http://someurl",
			},
			Valid: false,
		},
		{
			Name: "invalid missing location",
			Message: map[string]any{
				"checksum": "deadbeef",
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid checksum exceeds maxLength 32",
			Message: messages.PublishFirmwareRequest{
				Location:      "http://someurl",
				Retries:       ptr(int32(5)),
				Checksum:      longString(33),
				RequestID:     42,
				RetryInterval: ptr(int32(300)),
			},
			Valid: false,
		},
		{
			Name: "invalid location exceeds maxLength 512",
			Message: messages.PublishFirmwareRequest{
				Location:      longString(513),
				Retries:       ptr(int32(5)),
				Checksum:      "deadbeef",
				RequestID:     42,
				RetryInterval: ptr(int32(300)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid retryInterval below minimum")
	skipSchemaOverride201(t, "invalid requestId below minimum")
	skipSchemaOverride201(t, "invalid retries below minimum")
}

func TestPublishFirmware201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "PublishFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.PublishFirmwareResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo201("ok"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.PublishFirmwareResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.PublishFirmwareResponse{
				Status: "invalidStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPublishFirmware201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.PublishFirmware)
}
