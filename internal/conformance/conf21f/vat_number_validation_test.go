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

func TestVatNumberValidation21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "VatNumberValidation", "request")

	cases := []conformance.ValidationCase{
		{Name: "valid", Message: messages.VatNumberValidationRequest{VatNumber: "DE123456789"}, Valid: true},
		{Name: "missing vatNumber", Message: map[string]any{}, Valid: false},
		{Name: "exceeds maxLength vatNumber", Message: messages.VatNumberValidationRequest{VatNumber: strings.Repeat("x", 21)}, Valid: false},
		{Name: "exceeds maxLength customData.vendorId", Message: messages.VatNumberValidationRequest{VatNumber: "DE123", CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)}}, Valid: false},
	}
	conformance.RunValidationTable(t, validator, cases)
}

func TestVatNumberValidation21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "VatNumberValidation", "response")

	cases := []conformance.ValidationCase{
		{Name: "valid", Message: messages.VatNumberValidationResponse{Status: "Accepted", VatNumber: "DE123456789"}, Valid: true},
		{Name: "missing required fields", Message: map[string]any{}, Valid: false},
		{Name: "invalid enum status", Message: messages.VatNumberValidationResponse{Status: "Nope", VatNumber: "DE123"}, Valid: false},
		{Name: "exceeds maxLength vatNumber", Message: messages.VatNumberValidationResponse{Status: "Accepted", VatNumber: strings.Repeat("x", 21)}, Valid: false},
	}
	conformance.RunValidationTable(t, validator, cases)
}

func TestVatNumberValidation21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.VatNumberValidation)
}
