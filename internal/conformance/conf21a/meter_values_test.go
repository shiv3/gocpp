package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestMeterValues21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.MeterValuesRequest{
				EVSEID: 1,
				MeterValue: []messages.MeterValueType{
					{
						SampledValue: []messages.SampledValueType{
							{Value: dec()},
						},
						Timestamp: testTime(),
					},
				},
			},
			Valid: true,
		},
		{
			Name: "missing evseId",
			Message: map[string]any{
				"meterValue": []map[string]any{
					{
						"sampledValue": []map[string]any{{"value": 1}},
						"timestamp":    testTime(),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "missing meterValue.sampledValue",
			Message: map[string]any{
				"evseId": 1,
				"meterValue": []map[string]any{
					{"timestamp": testTime()},
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength meterValue.sampledValue.unitOfMeasure.unit",
			Message: messages.MeterValuesRequest{
				EVSEID: 1,
				MeterValue: []messages.MeterValueType{
					{
						SampledValue: []messages.SampledValueType{
							{
								UnitOfMeasure: &messages.UnitOfMeasureType{Unit: ptr(longString(21))},
								Value:         dec(),
							},
						},
						Timestamp: testTime(),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum meterValue.sampledValue.measurand",
			Message: messages.MeterValuesRequest{
				EVSEID: 1,
				MeterValue: []messages.MeterValueType{
					{
						SampledValue: []messages.SampledValueType{
							{
								Measurand: ptr("BadEnum"),
								Value:     dec(),
							},
						},
						Timestamp: testTime(),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "MeterValues", "request"), cases)
}

func TestMeterValues21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.MeterValuesResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.MeterValuesResponse{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "MeterValues", "response"), cases)
}

func TestMeterValues21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.MeterValues)
}
