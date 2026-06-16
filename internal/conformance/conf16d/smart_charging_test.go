package conf16d_test

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
)

func TestClearChargingProfile16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "ClearChargingProfile", "request")

	purpose := messages.ClearChargingProfileRequestChargingProfilePurposeChargePointMaxProfile
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfilePurpose: &purpose,
				ConnectorID:            int32Ptr(1),
				ID:                     int32Ptr(1),
				StackLevel:             int32Ptr(1),
			},
			Valid: true,
		},
		{
			Name: "valid id and connector request",
			Message: messages.ClearChargingProfileRequest{
				ConnectorID: int32Ptr(1),
				ID:          int32Ptr(1),
			},
			Valid: true,
		},
		{
			Name:    "valid empty request",
			Message: messages.ClearChargingProfileRequest{},
			Valid:   true,
		},
		{
			Name: "valid zero connectorId and stackLevel",
			Message: map[string]any{
				"connectorId": 0,
				"stackLevel":  0,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown chargingProfilePurpose enum",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfilePurpose: (*messages.ClearChargingProfileRequestChargingProfilePurpose)(stringPtr("invalidChargingProfilePurposeType")),
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId": -1,
			},
			Valid: false,
		},
		{
			Name: "invalid stackLevel below minimum",
			Message: map[string]any{
				"stackLevel": -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearChargingProfile16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "ClearChargingProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.ClearChargingProfileResponse{
				Status: messages.ClearChargingProfileResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid unknown response",
			Message: messages.ClearChargingProfileResponse{
				Status: messages.ClearChargingProfileResponseStatusUnknown,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.ClearChargingProfileResponse{
				Status: messages.ClearChargingProfileResponseStatus("invalidClearChargingProfileStatus"),
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearChargingProfile16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.ClearChargingProfile)
}

func TestGetCompositeSchedule16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "GetCompositeSchedule", "request")

	unit := messages.GetCompositeScheduleRequestChargingRateUnitW
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.GetCompositeScheduleRequest{
				ChargingRateUnit: &unit,
				ConnectorID:      1,
				Duration:         600,
			},
			Valid: true,
		},
		{
			Name: "valid without chargingRateUnit",
			Message: messages.GetCompositeScheduleRequest{
				ConnectorID: 1,
				Duration:    600,
			},
			Valid: true,
		},
		{
			Name: "valid connectorId zero",
			Message: messages.GetCompositeScheduleRequest{
				ConnectorID: 0,
				Duration:    600,
			},
			Valid: true,
		},
		{
			Name: "valid duration zero",
			Message: messages.GetCompositeScheduleRequest{
				ConnectorID: 1,
				Duration:    0,
			},
			Valid: true,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"duration": 600,
			},
			Valid: false,
		},
		{
			Name: "invalid missing duration",
			Message: map[string]any{
				"connectorId": 1,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing connectorId and duration",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown chargingRateUnit enum",
			Message: messages.GetCompositeScheduleRequest{
				ChargingRateUnit: (*messages.GetCompositeScheduleRequestChargingRateUnit)(stringPtr("invalidChargingRateUnit")),
				ConnectorID:      1,
				Duration:         600,
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId": -1,
				"duration":    600,
			},
			Valid: false,
		},
		{
			Name: "invalid duration below minimum",
			Message: map[string]any{
				"connectorId": 1,
				"duration":    -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCompositeSchedule16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "GetCompositeSchedule", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted full response",
			Message: messages.GetCompositeScheduleResponse{
				ChargingSchedule: &[]messages.ChargingSchedule{testChargingSchedule()}[0],
				ConnectorID:      int32Ptr(1),
				ScheduleStart:    timePtr(testTime()),
				Status:           messages.GetCompositeScheduleResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid accepted minimal response",
			Message: messages.GetCompositeScheduleResponse{
				Status: messages.GetCompositeScheduleResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.GetCompositeScheduleResponse{
				Status: messages.GetCompositeScheduleResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "valid connectorId zero",
			Message: messages.GetCompositeScheduleResponse{
				ConnectorID: int32Ptr(0),
				Status:      messages.GetCompositeScheduleResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.GetCompositeScheduleResponse{
				Status: messages.GetCompositeScheduleResponseStatus("invalidGetCompositeScheduleStatus"),
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedule unknown chargingRateUnit enum",
			Message: map[string]any{
				"status": messages.GetCompositeScheduleResponseStatusAccepted,
				"chargingSchedule": map[string]any{
					"chargingRateUnit":       "invalidChargingRateUnit",
					"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedule missing chargingRateUnit",
			Message: map[string]any{
				"status": messages.GetCompositeScheduleResponseStatusAccepted,
				"chargingSchedule": map[string]any{
					"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedule missing chargingSchedulePeriod",
			Message: map[string]any{
				"status": messages.GetCompositeScheduleResponseStatusAccepted,
				"chargingSchedule": map[string]any{
					"chargingRateUnit": "W",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedulePeriod missing startPeriod",
			Message: map[string]any{
				"status": messages.GetCompositeScheduleResponseStatusAccepted,
				"chargingSchedule": map[string]any{
					"chargingRateUnit":       "W",
					"chargingSchedulePeriod": []map[string]any{{"limit": 10.0}},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedulePeriod missing limit",
			Message: map[string]any{
				"status": messages.GetCompositeScheduleResponseStatusAccepted,
				"chargingSchedule": map[string]any{
					"chargingRateUnit":       "W",
					"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0}},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"status":      messages.GetCompositeScheduleResponseStatusAccepted,
				"connectorId": -1,
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedulePeriod empty",
			Message: map[string]any{
				"status": messages.GetCompositeScheduleResponseStatusAccepted,
				"chargingSchedule": map[string]any{
					"chargingRateUnit":       "W",
					"chargingSchedulePeriod": []map[string]any{},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCompositeSchedule16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.GetCompositeSchedule)
}

func TestSetChargingProfile16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "SetChargingProfile", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.SetChargingProfileRequest{
				ConnectorID:        1,
				CsChargingProfiles: fullCsChargingProfiles(),
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.SetChargingProfileRequest{
				ConnectorID:        1,
				CsChargingProfiles: testCsChargingProfiles(),
			},
			Valid: true,
		},
		{
			Name: "valid connectorId zero",
			Message: messages.SetChargingProfileRequest{
				ConnectorID:        0,
				CsChargingProfiles: testCsChargingProfiles(),
			},
			Valid: true,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing csChargingProfiles",
			Message: map[string]any{
				"connectorId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid csChargingProfiles missing chargingProfileId",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid csChargingProfiles missing stackLevel",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid csChargingProfiles missing chargingProfilePurpose",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":   1,
					"stackLevel":          1,
					"chargingProfileKind": "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid csChargingProfiles missing chargingProfileKind",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid csChargingProfiles missing chargingSchedule",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid unknown chargingProfilePurpose enum",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "invalidChargingProfilePurposeType",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid unknown chargingProfileKind enum",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "invalidChargingProfileKind",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid unknown recurrencyKind enum",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"recurrencyKind":         "invalidRecurrencyKind",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedule unknown chargingRateUnit enum",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "invalidChargingRateUnit",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedule missing chargingRateUnit",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedule missing chargingSchedulePeriod",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit": "W",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedulePeriod missing startPeriod",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedulePeriod missing limit",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId": -1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{{"startPeriod": 0, "limit": 10.0}},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid chargingSchedulePeriod empty",
			Message: map[string]any{
				"connectorId": 1,
				"csChargingProfiles": map[string]any{
					"chargingProfileId":      1,
					"stackLevel":             1,
					"chargingProfilePurpose": "ChargePointMaxProfile",
					"chargingProfileKind":    "Absolute",
					"chargingSchedule": map[string]any{
						"chargingRateUnit":       "W",
						"chargingSchedulePeriod": []map[string]any{},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetChargingProfile16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "SetChargingProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.SetChargingProfileResponse{
				Status: messages.SetChargingProfileResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.SetChargingProfileResponse{
				Status: messages.SetChargingProfileResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "valid not supported response",
			Message: messages.SetChargingProfileResponse{
				Status: messages.SetChargingProfileResponseStatusNotSupported,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.SetChargingProfileResponse{
				Status: messages.SetChargingProfileResponseStatus("invalidChargingProfileStatus"),
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetChargingProfile16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.SetChargingProfile)
}

func stringPtr(s string) *string {
	return &s
}
