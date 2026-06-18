package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSendLocalList21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{
					{
						IDToken: messages.IdTokenType{
							IDToken: "id-token",
							Type:    "Central",
						},
					},
				},
				UpdateType:    "Full",
				VersionNumber: 1,
			},
			Valid: true,
		},
		{
			Name: "missing updateType",
			Message: map[string]any{
				"versionNumber": 1,
			},
			Valid: false,
		},
		{
			Name: "missing localAuthorizationList.idToken",
			Message: map[string]any{
				"localAuthorizationList": []map[string]any{{}},
				"updateType":             "Full",
				"versionNumber":          1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength localAuthorizationList.idToken.idToken",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{
					{
						IDToken: messages.IdTokenType{
							IDToken: longString(256),
							Type:    "Central",
						},
					},
				},
				UpdateType:    "Full",
				VersionNumber: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum updateType",
			Message: messages.SendLocalListRequest{
				UpdateType:    "BadEnum",
				VersionNumber: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "SendLocalList", "request"), cases)
}

func TestSendLocalList21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SendLocalListResponse{
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
			Message: messages.SendLocalListResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: longString(21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.SendLocalListResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "SendLocalList", "response"), cases)
}

func TestSendLocalList21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.SendLocalList)
}
