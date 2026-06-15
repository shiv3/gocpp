package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestSetDisplayMessage21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SetDisplayMessage", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetDisplayMessageRequest{
				Message: testMessageInfo(),
			},
			Valid: true,
		},
		{
			Name:    "missing message",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "missing message id",
			Message: map[string]any{
				"message": map[string]any{
					"priority": "NormalCycle",
					"message": map[string]any{
						"content": "Display message",
						"format":  "UTF8",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "missing message priority",
			Message: map[string]any{
				"message": map[string]any{
					"id": 1,
					"message": map[string]any{
						"content": "Display message",
						"format":  "UTF8",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "missing message message",
			Message: map[string]any{
				"message": map[string]any{
					"id":       1,
					"priority": "NormalCycle",
				},
			},
			Valid: false,
		},
		{
			Name: "missing message message format",
			Message: map[string]any{
				"message": map[string]any{
					"id":       1,
					"priority": "NormalCycle",
					"message": map[string]any{
						"content": "Display message",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "missing message message content",
			Message: map[string]any{
				"message": map[string]any{
					"id":       1,
					"priority": "NormalCycle",
					"message": map[string]any{
						"format": "UTF8",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength message content",
			Message: messages.SetDisplayMessageRequest{
				Message: messages.MessageInfoType{
					ID:       1,
					Priority: "NormalCycle",
					Message: messages.MessageContentType{
						Content: longString(1025),
						Format:  "UTF8",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum message priority",
			Message: messages.SetDisplayMessageRequest{
				Message: messages.MessageInfoType{
					ID:       1,
					Priority: "invalidMessagePriority",
					Message:  testMessageContent(),
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDisplayMessage21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SetDisplayMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetDisplayMessageResponse{
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
			Name: "exceeds maxLength statusInfo reasonCode",
			Message: messages.SetDisplayMessageResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.SetDisplayMessageResponse{
				Status: "invalidDisplayMessageStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDisplayMessage21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.SetDisplayMessage)
}
