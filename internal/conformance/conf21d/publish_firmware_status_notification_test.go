package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestPublishFirmwareStatusNotification21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "PublishFirmwareStatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Location:  []string{"https://example.com/firmware.bin"},
				RequestID: int32Ptr(1),
				Status:    "Downloaded",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength location",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Location: []string{longString(2001)},
				Status:   "Downloaded",
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "InvalidPublishFirmwareStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPublishFirmwareStatusNotification21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "PublishFirmwareStatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.PublishFirmwareStatusNotificationResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.PublishFirmwareStatusNotificationResponse{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPublishFirmwareStatusNotification21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.PublishFirmwareStatusNotification)
}
