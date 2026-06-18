package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestClearDisplayMessage21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearDisplayMessage", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearDisplayMessageRequest{
				ID: 42,
			},
			Valid: true,
		},
		{
			Name:    "missing id",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.ClearDisplayMessageRequest{
				CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				ID:         42,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearDisplayMessage21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearDisplayMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearDisplayMessageResponse{
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
			Message: messages.ClearDisplayMessageResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.ClearDisplayMessageResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearDisplayMessage21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.ClearDisplayMessage)
}
