package conf201e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestSendLocalList201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "SendLocalList", "request")

	authData := testAuthorizationData201e()
	cases := []conformance.ValidationCase{
		{
			Name: "valid differential request with localAuthorizationList",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{authData},
				UpdateType:             "Differential",
				VersionNumber:          42,
			},
			Valid: true,
		},
		{
			Name: "valid full request with localAuthorizationList",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{authData},
				UpdateType:             "Full",
				VersionNumber:          42,
			},
			Valid: true,
		},
		{
			Name: "valid empty localAuthorizationList omitted",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{},
				UpdateType:             "Differential",
				VersionNumber:          42,
			},
			Valid: true,
		},
		{
			Name: "valid request without localAuthorizationList",
			Message: messages.SendLocalListRequest{
				UpdateType:    "Differential",
				VersionNumber: 42,
			},
			Valid: true,
		},
		{
			Name: "valid zero versionNumber request",
			Message: messages.SendLocalListRequest{
				UpdateType: "Differential",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing versionNumber",
			Message: map[string]any{
				"updateType": "Differential",
			},
			Valid: false,
		},
		{
			Name: "invalid missing updateType",
			Message: map[string]any{
				"versionNumber": 42,
			},
			Valid: false,
		},
		{
			Name: "invalid versionNumber below minimum",
			Message: map[string]any{
				"versionNumber": -1,
				"updateType":    "Differential",
			},
			Valid: false,
		},
		{
			Name: "invalid updateType enum",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{authData},
				UpdateType:             "invalidUpdateType",
				VersionNumber:          42,
			},
			Valid: false,
		},
		{
			Name: "invalid localAuthorizationList.idToken missing type",
			Message: messages.SendLocalListRequest{
				LocalAuthorizationList: []messages.AuthorizationData{
					{
						IDToken: messages.IdTokenType{IDToken: "tokenWithoutType"},
					},
				},
				UpdateType:    "Differential",
				VersionNumber: 42,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSendLocalList201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "SendLocalList", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SendLocalListResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SendLocalListResponse{
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
			Message: messages.SendLocalListResponse{
				Status:     "invalidStatus",
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
		{
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSendLocalList201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.SendLocalList)
}
