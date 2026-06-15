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

func TestReset201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "Reset", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid immediate request with evseId",
			Message: messages.ResetRequest{
				EVSEID: int32Ptr201e(42),
				Type:   "Immediate",
			},
			Valid: true,
		},
		{
			Name: "valid on idle request with evseId",
			Message: messages.ResetRequest{
				EVSEID: int32Ptr201e(42),
				Type:   "OnIdle",
			},
			Valid: true,
		},
		{
			Name: "valid immediate request",
			Message: messages.ResetRequest{
				Type: "Immediate",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.ResetRequest{
				CustomData: testCustomData201e(),
				EVSEID:     int32Ptr201e(42),
				Type:       "Immediate",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing type",
			Message: map[string]any{},
			Valid:   false,
		},
		// TODO(parity): needs schema override for evseId minimum.
		{
			Name: "invalid type enum",
			Message: messages.ResetRequest{
				EVSEID: int32Ptr201e(42),
				Type:   "invalidResetType",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReset201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "Reset", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.ResetResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid rejected response with statusInfo",
			Message: messages.ResetResponse{
				Status:     "Rejected",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid scheduled response with statusInfo",
			Message: messages.ResetResponse{
				Status:     "Scheduled",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.ResetResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		// TODO(parity): needs schema override for empty statusInfo.reasonCode minLength.
		{
			Name: "invalid status enum",
			Message: messages.ResetResponse{
				Status:     "invalidResetStatus",
				StatusInfo: testStatusInfo201e(),
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

func TestReset201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.Reset)
}
