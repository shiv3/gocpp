package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func requestStartChargingProfile201() messages.ChargingProfileType {
	profile := chargingProfile201("TxProfile")
	profile.ChargingSchedule = []messages.ChargingScheduleType{
		{
			ChargingRateUnit: "A",
			ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{
				{
					StartPeriod: 0,
					Limit:       dec("16.0"),
				},
			},
		},
	}
	return profile
}

func TestRequestStartTransaction201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "RequestStartTransaction", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.RequestStartTransactionRequest{
				EVSEID:          ptr(int32(1)),
				RemoteStartID:   42,
				IDToken:         idToken201("KeyCode"),
				ChargingProfile: ptr(requestStartChargingProfile201()),
				GroupIDToken:    ptr(idToken201("ISO15693")),
			},
			Valid: true,
		},
		{
			Name: "valid without groupIdToken",
			Message: messages.RequestStartTransactionRequest{
				EVSEID:          ptr(int32(1)),
				RemoteStartID:   42,
				IDToken:         idToken201("KeyCode"),
				ChargingProfile: ptr(requestStartChargingProfile201()),
			},
			Valid: true,
		},
		{
			Name: "valid without chargingProfile",
			Message: messages.RequestStartTransactionRequest{
				EVSEID:        ptr(int32(1)),
				RemoteStartID: 42,
				IDToken:       idToken201("KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "valid without evseId",
			Message: messages.RequestStartTransactionRequest{
				RemoteStartID: 42,
				IDToken:       idToken201("KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "valid zero remoteStartId",
			Message: messages.RequestStartTransactionRequest{
				IDToken: idToken201("KeyCode"),
			},
			Valid: true,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid idToken type enum",
			Message: messages.RequestStartTransactionRequest{
				EVSEID:          ptr(int32(1)),
				RemoteStartID:   42,
				IDToken:         idToken201("invalidIdToken"),
				ChargingProfile: ptr(requestStartChargingProfile201()),
				GroupIDToken:    ptr(idToken201("ISO15693")),
			},
			Valid: false,
		},
		{
			Name: "invalid empty chargingProfile",
			Message: messages.RequestStartTransactionRequest{
				EVSEID:          ptr(int32(1)),
				RemoteStartID:   42,
				IDToken:         idToken201("KeyCode"),
				ChargingProfile: &messages.ChargingProfileType{},
				GroupIDToken:    ptr(idToken201("ISO15693")),
			},
			Valid: false,
		},
		{
			Name: "invalid groupIdToken type enum",
			Message: messages.RequestStartTransactionRequest{
				EVSEID:          ptr(int32(1)),
				RemoteStartID:   42,
				IDToken:         idToken201("KeyCode"),
				ChargingProfile: ptr(requestStartChargingProfile201()),
				GroupIDToken:    ptr(idToken201("invalidGroupIdToken")),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid evseId below minimum")
	skipSchemaOverride201(t, "invalid remoteStartId below minimum")
}

func TestRequestStartTransaction201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "RequestStartTransaction", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with transactionId and statusInfo",
			Message: messages.RequestStartTransactionResponse{
				Status:        "Accepted",
				TransactionID: ptr("12345"),
				StatusInfo:    statusInfo201("200"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response with transactionId",
			Message: messages.RequestStartTransactionResponse{
				Status:        "Accepted",
				TransactionID: ptr("12345"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.RequestStartTransactionResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.RequestStartTransactionResponse{
				Status: "Rejected",
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
			Message: messages.RequestStartTransactionResponse{
				Status:        "invalidRequestStartStopStatus",
				TransactionID: ptr("12345"),
				StatusInfo:    statusInfo201("200"),
			},
			Valid: false,
		},
		{
			Name: "invalid transactionId exceeds maxLength 36",
			Message: messages.RequestStartTransactionResponse{
				Status:        "Accepted",
				TransactionID: ptr(longString(37)),
				StatusInfo:    statusInfo201("200"),
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":        "Accepted",
				"transactionId": "12345",
				"statusInfo":    map[string]any{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRequestStartTransaction201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.RequestStartTransaction)
}
