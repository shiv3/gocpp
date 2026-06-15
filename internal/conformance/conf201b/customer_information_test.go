package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestCustomerInformation201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid full request",
			Message: messages.CustomerInformationRequest{
				RequestID:          42,
				Report:             true,
				Clear:              true,
				CustomerIdentifier: ptr("0001"),
				IDToken: &messages.IdTokenType{
					IDToken: "1234",
					Type:    "KeyCode",
				},
				CustomerCertificate: ptr(certificateHashData()),
			},
			Valid: true,
		},
		{
			Name: "valid with idToken",
			Message: messages.CustomerInformationRequest{
				RequestID:          42,
				Report:             true,
				Clear:              true,
				CustomerIdentifier: ptr("0001"),
				IDToken: &messages.IdTokenType{
					IDToken: "1234",
					Type:    "KeyCode",
				},
			},
			Valid: true,
		},
		{
			Name: "valid with customerIdentifier",
			Message: messages.CustomerInformationRequest{
				RequestID:          42,
				Report:             true,
				Clear:              true,
				CustomerIdentifier: ptr("0001"),
			},
			Valid: true,
		},
		{
			Name: "valid required fields only",
			Message: messages.CustomerInformationRequest{
				RequestID: 42,
				Report:    true,
				Clear:     true,
			},
			Valid: true,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"report": true,
				"clear":  true,
			},
			Valid: false,
		},
		{
			Name: "invalid missing report",
			Message: map[string]any{
				"requestId": 42,
				"clear":     true,
			},
			Valid: false,
		},
		{
			Name: "invalid missing clear",
			Message: map[string]any{
				"requestId": 42,
				"report":    true,
			},
			Valid: false,
		},
		// TODO(parity): needs schema override; OCA schema has no minimum for requestId.
		{
			Name: "invalid customerIdentifier exceeds maxLength 64",
			Message: messages.CustomerInformationRequest{
				RequestID:          42,
				Report:             true,
				Clear:              true,
				CustomerIdentifier: ptr(longString(65)),
			},
			Valid: false,
		},
		{
			Name: "invalid idToken type enum",
			Message: messages.CustomerInformationRequest{
				RequestID: 42,
				Report:    true,
				Clear:     true,
				IDToken: &messages.IdTokenType{
					IDToken: "1234",
					Type:    "invalidTokenType",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid customerCertificate hashAlgorithm enum",
			Message: messages.CustomerInformationRequest{
				RequestID: 42,
				Report:    true,
				Clear:     true,
				CustomerCertificate: &messages.CertificateHashDataType{
					HashAlgorithm:  "invalidHashAlgorithm",
					IssuerNameHash: "hash00",
					IssuerKeyHash:  "hash01",
					SerialNumber:   "serial0",
				},
			},
			Valid: false,
		},
	}

	runValidation201(t, "CustomerInformation", "request", cases)
}

func TestCustomerInformation201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid accepted response",
			Message: messages.CustomerInformationResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.CustomerInformationResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid invalid response",
			Message: messages.CustomerInformationResponse{
				Status: "Invalid",
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
			Message: messages.CustomerInformationResponse{
				Status: "invalidCustomerInformationStatus",
			},
			Valid: false,
		},
	}

	runValidation201(t, "CustomerInformation", "response", cases)
}

func TestCustomerInformation201_Direction(t *testing.T) {
	assertCPRejectsWrongDirection(t, v201profiles.CustomerInformation)
}
