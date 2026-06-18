package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetCompositeSchedule21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "GetCompositeSchedule", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.GetCompositeScheduleRequest{
				ChargingRateUnit: strPtr21("W"),
				CustomData:       customData21(),
				Duration:         600,
				EVSEID:           1,
			},
			Valid: true,
		},
		{
			Name: "missing duration",
			Message: map[string]any{
				"chargingRateUnit": "W",
				"customData":       customDataMap21(),
				"evseId":           1,
			},
			Valid: false,
		},
		{
			Name: "missing evseId",
			Message: map[string]any{
				"chargingRateUnit": "W",
				"customData":       customDataMap21(),
				"duration":         600,
			},
			Valid: false,
		},
		{
			Name: "invalid chargingRateUnit enum",
			Message: messages.GetCompositeScheduleRequest{
				ChargingRateUnit: strPtr21("kW"),
				Duration:         600,
				EVSEID:           1,
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"duration":   600,
				"evseId":     1,
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"duration":   600,
				"evseId":     1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCompositeSchedule21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "GetCompositeSchedule", "response")

	validSchedule := map[string]any{
		"chargingRateUnit": "W",
		"chargingSchedulePeriod": []any{
			map[string]any{
				"customData":    customDataMap21(),
				"operationMode": "ChargingOnly",
				"startPeriod":   0,
			},
		},
		"customData":    customDataMap21(),
		"duration":      600,
		"evseId":        1,
		"scheduleStart": testTime21().Format(timeFormatRFC3339Nano21),
	}

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.GetCompositeScheduleResponse{
				CustomData: customData21(),
				Schedule: &messages.CompositeScheduleType{
					ChargingRateUnit: "W",
					ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{
						{
							CustomData:    customData21(),
							OperationMode: strPtr21("ChargingOnly"),
							StartPeriod:   0,
						},
					},
					CustomData:    customData21(),
					Duration:      600,
					EVSEID:        1,
					ScheduleStart: testTime21(),
				},
				Status:     "Accepted",
				StatusInfo: statusInfo21(),
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{"customData": customDataMap21(), "schedule": validSchedule, "statusInfo": statusInfoMap21()},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.GetCompositeScheduleResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
		{
			Name: "missing schedule.evseId",
			Message: map[string]any{
				"schedule": without21(validSchedule, "evseId"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing schedule.duration",
			Message: map[string]any{
				"schedule": without21(validSchedule, "duration"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing schedule.scheduleStart",
			Message: map[string]any{
				"schedule": without21(validSchedule, "scheduleStart"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing schedule.chargingRateUnit",
			Message: map[string]any{
				"schedule": without21(validSchedule, "chargingRateUnit"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing schedule.chargingSchedulePeriod",
			Message: map[string]any{
				"schedule": without21(validSchedule, "chargingSchedulePeriod"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid schedule.chargingRateUnit enum",
			Message: map[string]any{
				"schedule": with21(validSchedule, "kW", "chargingRateUnit"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing schedule.chargingSchedulePeriod.startPeriod",
			Message: map[string]any{
				"schedule": without21(validSchedule, "chargingSchedulePeriod", 0, "startPeriod"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid schedule.chargingSchedulePeriod.operationMode enum",
			Message: map[string]any{
				"schedule": with21(validSchedule, "InvalidMode", "chargingSchedulePeriod", 0, "operationMode"),
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"additionalInfo": "details", "customData": customDataMap21()},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.reasonCode exceeds maxLength",
			Message: messages.GetCompositeScheduleResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.additionalInfo exceeds maxLength",
			Message: messages.GetCompositeScheduleResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: strPtr21(strings.Repeat("x", 1025)),
					ReasonCode:     "OK",
				},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"status":     "Accepted",
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"status":     "Accepted",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCompositeSchedule21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetCompositeSchedule)
}
