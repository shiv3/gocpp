package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestFirmwareStatusNotification201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid with requestId",
			Message: messages.FirmwareStatusNotificationRequest{
				Status:    "Downloaded",
				RequestID: ptr(int32(42)),
			},
			Valid: true,
		},
		{
			Name: "valid status only",
			Message: messages.FirmwareStatusNotificationRequest{
				Status: "Downloaded",
			},
			Valid: true,
		},
		{
			Name: "invalid missing status with requestId",
			Message: map[string]any{
				"requestId": 42,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		// TODO(parity): needs schema override; OCA schema has no minimum for requestId.
		{
			Name: "invalid status enum",
			Message: messages.FirmwareStatusNotificationRequest{
				Status: "invalidFirmwareStatus",
			},
			Valid: false,
		},
	}

	runValidation201(t, "FirmwareStatusNotification", "request", cases)
}

func TestFirmwareStatusNotification201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name:    "valid empty response",
			Message: messages.FirmwareStatusNotificationResponse{},
			Valid:   true,
		},
	}

	runValidation201(t, "FirmwareStatusNotification", "response", cases)
}

func TestFirmwareStatusNotification201_Direction(t *testing.T) {
	assertCSMSRejectsWrongDirection(t, v201profiles.FirmwareStatusNotification)
}
