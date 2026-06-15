package conf201f

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func firmware201() messages.FirmwareType {
	return messages.FirmwareType{
		Location:           "https://someurl",
		RetrieveDateTime:   fixedTime201(),
		InstallDateTime:    ptr(fixedTime201()),
		SigningCertificate: ptr("1337c0de"),
		Signature:          ptr("deadc0de"),
	}
}

func TestUpdateFirmware201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "UpdateFirmware", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.UpdateFirmwareRequest{
				Retries:       ptr(int32(5)),
				RetryInterval: ptr(int32(300)),
				RequestID:     42,
				Firmware:      firmware201(),
			},
			Valid: true,
		},
		{
			Name: "valid without retryInterval",
			Message: messages.UpdateFirmwareRequest{
				Retries:   ptr(int32(5)),
				RequestID: 42,
				Firmware:  firmware201(),
			},
			Valid: true,
		},
		{
			Name: "valid without retries",
			Message: messages.UpdateFirmwareRequest{
				RequestID: 42,
				Firmware:  firmware201(),
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.UpdateFirmwareRequest{
				Firmware: firmware201(),
			},
			Valid: true,
		},
		{
			Name: "valid required firmware fields",
			Message: messages.UpdateFirmwareRequest{
				Retries:       ptr(int32(5)),
				RetryInterval: ptr(int32(300)),
				RequestID:     42,
				Firmware: messages.FirmwareType{
					Location:         "https://someurl",
					RetrieveDateTime: fixedTime201(),
				},
			},
			Valid: true,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing firmware.location",
			Message: map[string]any{
				"retries":       5,
				"retryInterval": 300,
				"requestId":     42,
				"firmware": map[string]any{
					"retrieveDateTime":   fixedTime201(),
					"installDateTime":    fixedTime201(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing firmware.retrieveDateTime",
			Message: map[string]any{
				"retries":       5,
				"retryInterval": 300,
				"requestId":     42,
				"firmware": map[string]any{
					"location":           "https://someurl",
					"installDateTime":    fixedTime201(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid firmware.location exceeds maxLength 512",
			Message: messages.UpdateFirmwareRequest{
				Retries:       ptr(int32(5)),
				RetryInterval: ptr(int32(300)),
				RequestID:     42,
				Firmware: func() messages.FirmwareType {
					fw := firmware201()
					fw.Location = longString(513)
					return fw
				}(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid retries below minimum")
	skipSchemaOverride201(t, "invalid retryInterval below minimum")
	skipSchemaOverride201(t, "invalid requestId below minimum")
}

func TestUpdateFirmware201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "UpdateFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.UpdateFirmwareResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo201("ok"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response with reason only",
			Message: messages.UpdateFirmwareResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: "ok"},
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.UpdateFirmwareResponse{
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
			Message: messages.UpdateFirmwareResponse{
				Status: "invalidFirmwareUpdateStatus",
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

func TestUpdateFirmware201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.UpdateFirmware)
}
