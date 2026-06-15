package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func validReportDERControlPayload21() map[string]any {
	return map[string]any{
		"curve": []any{
			map[string]any{
				"customData":   customDataMap21(),
				"curveType":    "VoltWatt",
				"id":           "curve-1",
				"isDefault":    true,
				"isSuperseded": false,
				"curve": map[string]any{
					"customData": customDataMap21(),
					"curveData": []any{
						map[string]any{"customData": customDataMap21(), "x": 1, "y": 2},
					},
					"priority": 1,
					"voltageParams": map[string]any{
						"customData":           customDataMap21(),
						"powerDuringCessation": "Active",
					},
					"yUnit": "PctMaxW",
				},
			},
		},
		"customData": customDataMap21(),
		"enterService": []any{
			map[string]any{
				"customData": customDataMap21(),
				"id":         "enter-service-1",
				"enterService": map[string]any{
					"customData":  customDataMap21(),
					"highFreq":    61,
					"highVoltage": 240,
					"lowFreq":     59,
					"lowVoltage":  220,
					"priority":    1,
				},
			},
		},
		"fixedPFAbsorb": []any{
			map[string]any{
				"customData":   customDataMap21(),
				"id":           "fixed-pf-1",
				"isDefault":    true,
				"isSuperseded": false,
				"fixedPF": map[string]any{
					"customData":   customDataMap21(),
					"displacement": 1,
					"excitation":   true,
					"priority":     1,
				},
			},
		},
		"fixedPFInject": []any{
			map[string]any{
				"customData":   customDataMap21(),
				"id":           "fixed-pf-inject-1",
				"isDefault":    true,
				"isSuperseded": false,
				"fixedPF": map[string]any{
					"customData":   customDataMap21(),
					"displacement": 1,
					"excitation":   false,
					"priority":     1,
				},
			},
		},
		"fixedVar": []any{
			map[string]any{
				"customData":   customDataMap21(),
				"id":           "fixed-var-1",
				"isDefault":    true,
				"isSuperseded": false,
				"fixedVar": map[string]any{
					"customData": customDataMap21(),
					"priority":   1,
					"setpoint":   10,
					"unit":       "PctMaxVar",
				},
			},
		},
		"freqDroop": []any{
			map[string]any{
				"customData":   customDataMap21(),
				"id":           "freq-droop-1",
				"isDefault":    true,
				"isSuperseded": false,
				"freqDroop": map[string]any{
					"customData":   customDataMap21(),
					"overDroop":    1,
					"overFreq":     61,
					"priority":     1,
					"responseTime": 2,
					"underDroop":   1,
					"underFreq":    59,
				},
			},
		},
		"gradient": []any{
			map[string]any{
				"customData": customDataMap21(),
				"id":         "gradient-1",
				"gradient": map[string]any{
					"customData":   customDataMap21(),
					"gradient":     1,
					"priority":     1,
					"softGradient": 2,
				},
			},
		},
		"limitMaxDischarge": []any{
			map[string]any{
				"customData":   customDataMap21(),
				"id":           "limit-max-discharge-1",
				"isDefault":    true,
				"isSuperseded": false,
				"limitMaxDischarge": map[string]any{
					"customData": customDataMap21(),
					"priority":   1,
				},
			},
		},
		"requestId": 1,
		"tbc":       true,
	}
}

func TestReportDERControl21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "ReportDERControl", "request")

	validPayload := validReportDERControlPayload21()

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.ReportDERControlRequest{
				Curve:             []messages.DERCurveGetType{derCurveGet21()},
				CustomData:        customData21(),
				EnterService:      []messages.EnterServiceGetType{enterService21()},
				FixedPFAbsorb:     []messages.FixedPFGetType{fixedPF21()},
				FixedPFInject:     []messages.FixedPFGetType{fixedPF21()},
				FixedVar:          []messages.FixedVarGetType{fixedVar21()},
				FreqDroop:         []messages.FreqDroopGetType{freqDroop21()},
				Gradient:          []messages.GradientGetType{gradient21()},
				LimitMaxDischarge: []messages.LimitMaxDischargeGetType{limitMaxDischarge21()},
				RequestID:         1,
				Tbc:               boolPtr21(true),
			},
			Valid: true,
		},
		{Name: "missing requestId", Message: without21(validPayload, "requestId"), Valid: false},
		{Name: "missing curve.id", Message: without21(validPayload, "curve", 0, "id"), Valid: false},
		{Name: "missing curve.curveType", Message: without21(validPayload, "curve", 0, "curveType"), Valid: false},
		{Name: "missing curve.isDefault", Message: without21(validPayload, "curve", 0, "isDefault"), Valid: false},
		{Name: "missing curve.isSuperseded", Message: without21(validPayload, "curve", 0, "isSuperseded"), Valid: false},
		{Name: "missing curve.curve", Message: without21(validPayload, "curve", 0, "curve"), Valid: false},
		{Name: "curve.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "curve", 0, "id"), Valid: false},
		{Name: "invalid curve.curveType enum", Message: with21(validPayload, "InvalidControl", "curve", 0, "curveType"), Valid: false},
		{Name: "missing curve.curve.priority", Message: without21(validPayload, "curve", 0, "curve", "priority"), Valid: false},
		{Name: "missing curve.curve.yUnit", Message: without21(validPayload, "curve", 0, "curve", "yUnit"), Valid: false},
		{Name: "missing curve.curve.curveData", Message: without21(validPayload, "curve", 0, "curve", "curveData"), Valid: false},
		{Name: "invalid curve.curve.yUnit enum", Message: with21(validPayload, "kW", "curve", 0, "curve", "yUnit"), Valid: false},
		{Name: "missing curve.curve.curveData.x", Message: without21(validPayload, "curve", 0, "curve", "curveData", 0, "x"), Valid: false},
		{Name: "missing curve.curve.curveData.y", Message: without21(validPayload, "curve", 0, "curve", "curveData", 0, "y"), Valid: false},
		{Name: "invalid curve.curve.voltageParams.powerDuringCessation enum", Message: with21(validPayload, "Apparent", "curve", 0, "curve", "voltageParams", "powerDuringCessation"), Valid: false},
		{Name: "missing enterService.id", Message: without21(validPayload, "enterService", 0, "id"), Valid: false},
		{Name: "missing enterService.enterService", Message: without21(validPayload, "enterService", 0, "enterService"), Valid: false},
		{Name: "enterService.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "enterService", 0, "id"), Valid: false},
		{Name: "missing enterService.enterService.priority", Message: without21(validPayload, "enterService", 0, "enterService", "priority"), Valid: false},
		{Name: "missing enterService.enterService.highVoltage", Message: without21(validPayload, "enterService", 0, "enterService", "highVoltage"), Valid: false},
		{Name: "missing enterService.enterService.lowVoltage", Message: without21(validPayload, "enterService", 0, "enterService", "lowVoltage"), Valid: false},
		{Name: "missing enterService.enterService.highFreq", Message: without21(validPayload, "enterService", 0, "enterService", "highFreq"), Valid: false},
		{Name: "missing enterService.enterService.lowFreq", Message: without21(validPayload, "enterService", 0, "enterService", "lowFreq"), Valid: false},
		{Name: "missing fixedPF.id", Message: without21(validPayload, "fixedPFAbsorb", 0, "id"), Valid: false},
		{Name: "missing fixedPF.isDefault", Message: without21(validPayload, "fixedPFAbsorb", 0, "isDefault"), Valid: false},
		{Name: "missing fixedPF.isSuperseded", Message: without21(validPayload, "fixedPFAbsorb", 0, "isSuperseded"), Valid: false},
		{Name: "missing fixedPF.fixedPF", Message: without21(validPayload, "fixedPFAbsorb", 0, "fixedPF"), Valid: false},
		{Name: "fixedPF.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "fixedPFAbsorb", 0, "id"), Valid: false},
		{Name: "missing fixedPF.fixedPF.priority", Message: without21(validPayload, "fixedPFAbsorb", 0, "fixedPF", "priority"), Valid: false},
		{Name: "missing fixedPF.fixedPF.displacement", Message: without21(validPayload, "fixedPFAbsorb", 0, "fixedPF", "displacement"), Valid: false},
		{Name: "missing fixedPF.fixedPF.excitation", Message: without21(validPayload, "fixedPFAbsorb", 0, "fixedPF", "excitation"), Valid: false},
		{Name: "missing fixedVar.id", Message: without21(validPayload, "fixedVar", 0, "id"), Valid: false},
		{Name: "missing fixedVar.isDefault", Message: without21(validPayload, "fixedVar", 0, "isDefault"), Valid: false},
		{Name: "missing fixedVar.isSuperseded", Message: without21(validPayload, "fixedVar", 0, "isSuperseded"), Valid: false},
		{Name: "missing fixedVar.fixedVar", Message: without21(validPayload, "fixedVar", 0, "fixedVar"), Valid: false},
		{Name: "fixedVar.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "fixedVar", 0, "id"), Valid: false},
		{Name: "missing fixedVar.fixedVar.priority", Message: without21(validPayload, "fixedVar", 0, "fixedVar", "priority"), Valid: false},
		{Name: "missing fixedVar.fixedVar.setpoint", Message: without21(validPayload, "fixedVar", 0, "fixedVar", "setpoint"), Valid: false},
		{Name: "missing fixedVar.fixedVar.unit", Message: without21(validPayload, "fixedVar", 0, "fixedVar", "unit"), Valid: false},
		{Name: "invalid fixedVar.fixedVar.unit enum", Message: with21(validPayload, "kvar", "fixedVar", 0, "fixedVar", "unit"), Valid: false},
		{Name: "missing freqDroop.id", Message: without21(validPayload, "freqDroop", 0, "id"), Valid: false},
		{Name: "missing freqDroop.isDefault", Message: without21(validPayload, "freqDroop", 0, "isDefault"), Valid: false},
		{Name: "missing freqDroop.isSuperseded", Message: without21(validPayload, "freqDroop", 0, "isSuperseded"), Valid: false},
		{Name: "missing freqDroop.freqDroop", Message: without21(validPayload, "freqDroop", 0, "freqDroop"), Valid: false},
		{Name: "freqDroop.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "freqDroop", 0, "id"), Valid: false},
		{Name: "missing freqDroop.freqDroop.priority", Message: without21(validPayload, "freqDroop", 0, "freqDroop", "priority"), Valid: false},
		{Name: "missing freqDroop.freqDroop.overFreq", Message: without21(validPayload, "freqDroop", 0, "freqDroop", "overFreq"), Valid: false},
		{Name: "missing freqDroop.freqDroop.underFreq", Message: without21(validPayload, "freqDroop", 0, "freqDroop", "underFreq"), Valid: false},
		{Name: "missing freqDroop.freqDroop.overDroop", Message: without21(validPayload, "freqDroop", 0, "freqDroop", "overDroop"), Valid: false},
		{Name: "missing freqDroop.freqDroop.underDroop", Message: without21(validPayload, "freqDroop", 0, "freqDroop", "underDroop"), Valid: false},
		{Name: "missing freqDroop.freqDroop.responseTime", Message: without21(validPayload, "freqDroop", 0, "freqDroop", "responseTime"), Valid: false},
		{Name: "missing gradient.id", Message: without21(validPayload, "gradient", 0, "id"), Valid: false},
		{Name: "missing gradient.gradient", Message: without21(validPayload, "gradient", 0, "gradient"), Valid: false},
		{Name: "gradient.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "gradient", 0, "id"), Valid: false},
		{Name: "missing gradient.gradient.priority", Message: without21(validPayload, "gradient", 0, "gradient", "priority"), Valid: false},
		{Name: "missing gradient.gradient.gradient", Message: without21(validPayload, "gradient", 0, "gradient", "gradient"), Valid: false},
		{Name: "missing gradient.gradient.softGradient", Message: without21(validPayload, "gradient", 0, "gradient", "softGradient"), Valid: false},
		{Name: "missing limitMaxDischarge.id", Message: without21(validPayload, "limitMaxDischarge", 0, "id"), Valid: false},
		{Name: "missing limitMaxDischarge.isDefault", Message: without21(validPayload, "limitMaxDischarge", 0, "isDefault"), Valid: false},
		{Name: "missing limitMaxDischarge.isSuperseded", Message: without21(validPayload, "limitMaxDischarge", 0, "isSuperseded"), Valid: false},
		{Name: "missing limitMaxDischarge.limitMaxDischarge", Message: without21(validPayload, "limitMaxDischarge", 0, "limitMaxDischarge"), Valid: false},
		{Name: "limitMaxDischarge.id exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "limitMaxDischarge", 0, "id"), Valid: false},
		{Name: "missing limitMaxDischarge.limitMaxDischarge.priority", Message: without21(validPayload, "limitMaxDischarge", 0, "limitMaxDischarge", "priority"), Valid: false},
		{Name: "missing customData.vendorId", Message: with21(validPayload, map[string]any{}, "customData"), Valid: false},
		{Name: "customData.vendorId exceeds maxLength", Message: with21(validPayload, map[string]any{"vendorId": strings.Repeat("x", 256)}, "customData"), Valid: false},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReportDERControl21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "ReportDERControl", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.ReportDERControlResponse{
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

func TestReportDERControl21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.ReportDERControl)
}
