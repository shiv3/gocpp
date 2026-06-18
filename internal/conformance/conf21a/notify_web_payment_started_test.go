package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyWebPaymentStarted21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyWebPaymentStartedRequest{
				EVSEID:  1,
				Timeout: 60,
			},
			Valid: true,
		},
		{
			Name: "missing timeout",
			Message: map[string]any{
				"evseId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.NotifyWebPaymentStartedRequest{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
				EVSEID:     1,
				Timeout:    60,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "NotifyWebPaymentStarted", "request"), cases)
}

func TestNotifyWebPaymentStarted21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyWebPaymentStartedResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.NotifyWebPaymentStartedResponse{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "NotifyWebPaymentStarted", "response"), cases)
}

func TestNotifyWebPaymentStarted21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.NotifyWebPaymentStarted)
}
