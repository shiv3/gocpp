package conf201f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestUnpublishFirmware201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "UnpublishFirmware", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid checksum",
			Message: messages.UnpublishFirmwareRequest{
				Checksum: "deadc0de",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing checksum",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid checksum exceeds maxLength 32",
			Message: messages.UnpublishFirmwareRequest{
				Checksum: strings.Repeat("x", 33),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnpublishFirmware201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "UnpublishFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid unpublished response",
			Message: messages.UnpublishFirmwareResponse{
				Status: "Unpublished",
			},
			Valid: true,
		},
		{
			Name: "valid no firmware response",
			Message: messages.UnpublishFirmwareResponse{
				Status: "NoFirmware",
			},
			Valid: true,
		},
		{
			Name: "valid download ongoing response",
			Message: messages.UnpublishFirmwareResponse{
				Status: "DownloadOngoing",
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
			Message: messages.UnpublishFirmwareResponse{
				Status: "invalidUnpublishFirmwareStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnpublishFirmware201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.UnpublishFirmware)
}
