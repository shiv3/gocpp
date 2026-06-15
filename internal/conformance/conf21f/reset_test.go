package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestReset21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "Reset", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ResetRequest{
				EVSEID: int32Ptr21(1),
				Type:   "Immediate",
			},
			Valid: true,
		},
		{
			Name:    "missing type",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.ResetRequest{
				CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				Type:       "Immediate",
			},
			Valid: false,
		},
		{
			Name: "invalid enum type",
			Message: messages.ResetRequest{
				Type: "InvalidType",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReset21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "Reset", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ResetResponse{
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
			Message: messages.ResetResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.ResetResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReset21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.Reset)
}
