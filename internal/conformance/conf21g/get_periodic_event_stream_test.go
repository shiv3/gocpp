package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetPeriodicEventStream21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "GetPeriodicEventStream", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.GetPeriodicEventStreamRequest{
				CustomData: customData21(),
			},
			Valid: true,
		},
		{
			Name:    "missing customData.vendorId",
			Message: map[string]any{"customData": map[string]any{}},
			Valid:   false,
		},
		{
			Name:    "customData.vendorId exceeds maxLength",
			Message: map[string]any{"customData": map[string]any{"vendorId": strings.Repeat("x", 256)}},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetPeriodicEventStream21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "GetPeriodicEventStream", "response")

	validStreamData := map[string]any{
		"customData":           customDataMap21(),
		"id":                   1,
		"params":               map[string]any{"customData": customDataMap21(), "interval": 60, "values": 10},
		"variableMonitoringId": 2,
	}

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.GetPeriodicEventStreamResponse{
				ConstantStreamData: []messages.ConstantStreamDataType{
					{
						CustomData: customData21(),
						ID:         1,
						Params: messages.PeriodicEventStreamParamsType{
							CustomData: customData21(),
							Interval:   int32Ptr21(60),
							Values:     int32Ptr21(10),
						},
						VariableMonitoringID: 2,
					},
				},
				CustomData: customData21(),
			},
			Valid: true,
		},
		{
			Name: "missing constantStreamData.id",
			Message: map[string]any{
				"constantStreamData": []map[string]any{without21(validStreamData, "id")},
			},
			Valid: false,
		},
		{
			Name: "missing constantStreamData.variableMonitoringId",
			Message: map[string]any{
				"constantStreamData": []map[string]any{without21(validStreamData, "variableMonitoringId")},
			},
			Valid: false,
		},
		{
			Name: "missing constantStreamData.params",
			Message: map[string]any{
				"constantStreamData": []map[string]any{without21(validStreamData, "params")},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"constantStreamData": []map[string]any{validStreamData},
				"customData":         map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"constantStreamData": []map[string]any{validStreamData},
				"customData":         map[string]any{"vendorId": strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetPeriodicEventStream21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetPeriodicEventStream)
}
