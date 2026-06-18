package conf21c

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestClearCache21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearCache", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.ClearCacheRequest{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.ClearCacheRequest{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearCache21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearCache", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearCacheResponse{
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
			Name: "exceeds maxLength",
			Message: messages.ClearCacheResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo21(longString(21)),
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.ClearCacheResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearCache21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.ClearCache)
}
