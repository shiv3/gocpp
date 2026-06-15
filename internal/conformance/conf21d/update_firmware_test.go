package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestUpdateFirmware21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "UpdateFirmware", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UpdateFirmwareRequest{
				Firmware:  testFirmware(),
				RequestID: 1,
			},
			Valid: true,
		},
		{
			Name: "missing firmware",
			Message: map[string]any{
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength firmware.location",
			Message: messages.UpdateFirmwareRequest{
				Firmware: messages.FirmwareType{
					Location:         longString(2001),
					RetrieveDateTime: testTime(),
				},
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateFirmware21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "UpdateFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UpdateFirmwareResponse{
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
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.UpdateFirmwareResponse{
				Status:     "Accepted",
				StatusInfo: invalidStatusInfoReason(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.UpdateFirmwareResponse{
				Status: "InvalidUpdateFirmwareStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateFirmware21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.UpdateFirmware)
}
