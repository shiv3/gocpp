package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGet15118EVCertificate201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid install request",
			Message: messages.Get15118EVCertificateRequest{
				Iso15118SchemaVersion: "1.0",
				Action:                "Install",
				ExiRequest:            "deadbeef",
			},
			Valid: true,
		},
		{
			Name: "valid update request",
			Message: messages.Get15118EVCertificateRequest{
				Iso15118SchemaVersion: "1.0",
				Action:                "Update",
				ExiRequest:            "deadbeef",
			},
			Valid: true,
		},
		{
			Name: "invalid missing exiRequest",
			Message: map[string]any{
				"iso15118SchemaVersion": "1.0",
				"action":                "Install",
			},
			Valid: false,
		},
		{
			Name: "invalid missing iso15118SchemaVersion",
			Message: map[string]any{
				"action":     "Install",
				"exiRequest": "deadbeef",
			},
			Valid: false,
		},
		{
			Name: "invalid missing action",
			Message: map[string]any{
				"iso15118SchemaVersion": "1.0",
				"exiRequest":            "deadbeef",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid iso15118SchemaVersion exceeds maxLength 50",
			Message: messages.Get15118EVCertificateRequest{
				Iso15118SchemaVersion: longString(51),
				Action:                "Install",
				ExiRequest:            "deadbeef",
			},
			Valid: false,
		},
		{
			Name: "invalid action enum",
			Message: messages.Get15118EVCertificateRequest{
				Iso15118SchemaVersion: "1.0",
				Action:                "invalidCertificateAction",
				ExiRequest:            "deadbeef",
			},
			Valid: false,
		},
		{
			Name: "invalid exiRequest exceeds maxLength 5600",
			Message: messages.Get15118EVCertificateRequest{
				Iso15118SchemaVersion: "1.0",
				Action:                "Install",
				ExiRequest:            longString(5601),
			},
			Valid: false,
		},
	}

	runValidation201(t, "Get15118EVCertificate", "request", cases)
}

func TestGet15118EVCertificate201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.Get15118EVCertificateResponse{
				Status:      "Accepted",
				ExiResponse: "deadbeef",
				StatusInfo:  statusInfo("200"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.Get15118EVCertificateResponse{
				Status:      "Accepted",
				ExiResponse: "deadbeef",
			},
			Valid: true,
		},
		{
			Name: "invalid missing exiResponse",
			Message: map[string]any{
				"status": "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid missing status",
			Message: map[string]any{
				"exiResponse": "deadbeef",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.Get15118EVCertificateResponse{
				Status:      "invalidCertificateStatus",
				ExiResponse: "deadbeef",
				StatusInfo:  statusInfo("200"),
			},
			Valid: false,
		},
		{
			Name: "invalid exiResponse exceeds maxLength 7500",
			Message: messages.Get15118EVCertificateResponse{
				Status:      "Accepted",
				ExiResponse: longString(7501),
				StatusInfo:  statusInfo("200"),
			},
			Valid: false,
		},
		// TODO(parity): needs schema override; OCA schema has no minLength for statusInfo.reasonCode.
	}

	runValidation201(t, "Get15118EVCertificate", "response", cases)
}

func TestGet15118EVCertificate201_Direction(t *testing.T) {
	assertCSMSRejectsWrongDirection(t, v201profiles.Get15118EVCertificate)
}
