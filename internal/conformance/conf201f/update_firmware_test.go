package conf201f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func testFirmware201f() messages.FirmwareType {
	installDateTime := fixedTime201f()
	return messages.FirmwareType{
		Location:           "https://someurl",
		RetrieveDateTime:   fixedTime201f(),
		InstallDateTime:    &installDateTime,
		SigningCertificate: strPtr201f("1337c0de"),
		Signature:          strPtr201f("deadc0de"),
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
				Retries:       int32Ptr201f(5),
				RetryInterval: int32Ptr201f(300),
				RequestID:     42,
				Firmware:      testFirmware201f(),
			},
			Valid: true,
		},
		{
			Name: "valid without retryInterval",
			Message: messages.UpdateFirmwareRequest{
				Retries:   int32Ptr201f(5),
				RequestID: 42,
				Firmware:  testFirmware201f(),
			},
			Valid: true,
		},
		{
			Name: "valid without retries",
			Message: messages.UpdateFirmwareRequest{
				RequestID: 42,
				Firmware:  testFirmware201f(),
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.UpdateFirmwareRequest{
				Firmware: testFirmware201f(),
			},
			Valid: true,
		},
		{
			Name: "valid required firmware fields",
			Message: messages.UpdateFirmwareRequest{
				Retries:       int32Ptr201f(5),
				RetryInterval: int32Ptr201f(300),
				RequestID:     42,
				Firmware: messages.FirmwareType{
					Location:         "https://someurl",
					RetrieveDateTime: fixedTime201f(),
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"firmware": map[string]any{
					"location":         "https://someurl",
					"retrieveDateTime": fixedTime201f(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing firmware",
			Message: map[string]any{
				"requestId": 42,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
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
					"retrieveDateTime":   fixedTime201f(),
					"installDateTime":    fixedTime201f(),
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
					"installDateTime":    fixedTime201f(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid firmware.location exceeds maxLength 512",
			Message: messages.UpdateFirmwareRequest{
				Retries:       int32Ptr201f(5),
				RetryInterval: int32Ptr201f(300),
				RequestID:     42,
				Firmware: func() messages.FirmwareType {
					fw := testFirmware201f()
					fw.Location = strings.Repeat("x", 513)
					return fw
				}(),
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for retries minimum.
		// TODO(parity): needs schema override for retryInterval minimum.
		// TODO(parity): needs schema override for requestId minimum.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateFirmware201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "UpdateFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.UpdateFirmwareResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode:     "ok",
					AdditionalInfo: strPtr201f("someInfo"),
				},
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
		// TODO(parity): needs schema override for empty statusInfo.reasonCode minLength.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateFirmware201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.UpdateFirmware)
}
