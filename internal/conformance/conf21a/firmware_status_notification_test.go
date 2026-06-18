package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestFirmwareStatusNotification21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.FirmwareStatusNotificationRequest{
				Status: "Installed",
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
			Message: messages.FirmwareStatusNotificationRequest{
				Status: "BadEnum",
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo missing reasonCode",
			Message: map[string]any{
				"status":     "Installed",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.FirmwareStatusNotificationRequest{
				Status: "Installed",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.additionalInfo exceeds maxLength 1024",
			Message: messages.FirmwareStatusNotificationRequest{
				Status: "Installed",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: ptr(longString(1025)),
					ReasonCode:     "reason",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "FirmwareStatusNotification", "request"), cases)
}

func TestFirmwareStatusNotification21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name:    "valid response",
			Message: messages.FirmwareStatusNotificationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "FirmwareStatusNotification", "response"), cases)
}

func TestFirmwareStatusNotification21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.FirmwareStatusNotification)
}
