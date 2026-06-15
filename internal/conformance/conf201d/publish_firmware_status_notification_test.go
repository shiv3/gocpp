package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestPublishFirmwareStatusNotification201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "PublishFirmwareStatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid published with location and requestId",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status:    "Published",
				Location:  []string{"http://someUri"},
				RequestID: ptr(int32(42)),
			},
			Valid: true,
		},
		{
			Name: "valid published with location",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status:   "Published",
				Location: []string{"http://someUri"},
			},
			Valid: true,
		},
		{
			Name: "valid checksum verified with empty location omitted",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status:   "ChecksumVerified",
				Location: []string{},
			},
			Valid: true,
		},
		{
			Name: "valid checksum verified",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "ChecksumVerified",
			},
			Valid: true,
		},
		{
			Name: "valid downloaded",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "Downloaded",
			},
			Valid: true,
		},
		{
			Name: "valid download failed",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "DownloadFailed",
			},
			Valid: true,
		},
		{
			Name: "valid downloading",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "Downloading",
			},
			Valid: true,
		},
		{
			Name: "valid download scheduled",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "DownloadScheduled",
			},
			Valid: true,
		},
		{
			Name: "valid download paused",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "DownloadPaused",
			},
			Valid: true,
		},
		{
			Name: "valid invalid checksum",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "InvalidChecksum",
			},
			Valid: true,
		},
		{
			Name: "valid idle",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "Idle",
			},
			Valid: true,
		},
		{
			Name: "valid publish failed",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "PublishFailed",
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
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status: "invalidStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid location exceeds maxLength 512",
			Message: messages.PublishFirmwareStatusNotificationRequest{
				Status:    "Published",
				Location:  []string{longString(513)},
				RequestID: ptr(int32(42)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid requestId below minimum")
}

func TestPublishFirmwareStatusNotification201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "PublishFirmwareStatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.PublishFirmwareStatusNotificationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPublishFirmwareStatusNotification201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.PublishFirmwareStatusNotification)
}
