package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestGetDERControl21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.GetDERControlRequest{
				ControlID:   ptr("control-1"),
				ControlType: ptr("EnterService"),
				IsDefault:   ptr(true),
				RequestID:   1,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing requestId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid controlId exceeds maxLength 36",
			Message: messages.GetDERControlRequest{
				ControlID: ptr(longString(37)),
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid controlType enum",
			Message: messages.GetDERControlRequest{
				ControlType: ptr("BadEnum"),
				RequestID:   1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "GetDERControl", "request"), cases)
}

func TestGetDERControl21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.GetDERControlResponse{
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
			Message: messages.GetDERControlResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo missing reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.GetDERControlResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.additionalInfo exceeds maxLength 1024",
			Message: messages.GetDERControlResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: ptr(longString(1025)),
					ReasonCode:     "reason",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "GetDERControl", "response"), cases)
}

func TestGetDERControl21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.GetDERControl)
}
