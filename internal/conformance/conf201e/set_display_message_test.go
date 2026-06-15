package conf201e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func testMessageInfo201e() messages.MessageInfoType {
	start := fixedTime201e()
	return messages.MessageInfoType{
		ID:            42,
		Priority:      "AlwaysFront",
		State:         strPtr201e("Idle"),
		StartDateTime: &start,
		Message: messages.MessageContentType{
			Format:  "UTF8",
			Content: "hello world",
		},
	}
}

func TestSetDisplayMessage201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetDisplayMessage", "request")

	invalidPriorityMessage := testMessageInfo201e()
	invalidPriorityMessage.Priority = "invalidPriority"
	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.SetDisplayMessageRequest{
				Message: testMessageInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.SetDisplayMessageRequest{
				CustomData: testCustomData201e(),
				Message:    testMessageInfo201e(),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing message",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid priority enum",
			Message: messages.SetDisplayMessageRequest{
				Message: invalidPriorityMessage,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDisplayMessage201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetDisplayMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SetDisplayMessageResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SetDisplayMessageResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid not supported message format response",
			Message: messages.SetDisplayMessageResponse{
				Status: "NotSupportedMessageFormat",
			},
			Valid: true,
		},
		{
			Name: "valid not supported state response",
			Message: messages.SetDisplayMessageResponse{
				Status: "NotSupportedState",
			},
			Valid: true,
		},
		{
			Name: "valid not supported priority response",
			Message: messages.SetDisplayMessageResponse{
				Status: "NotSupportedPriority",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.SetDisplayMessageResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid unknown transaction response",
			Message: messages.SetDisplayMessageResponse{
				Status: "UnknownTransaction",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.SetDisplayMessageResponse{
				Status: "invalidDisplayMessageStatus",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDisplayMessage201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.SetDisplayMessage)
}
