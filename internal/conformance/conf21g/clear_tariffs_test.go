package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestClearTariffs21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearTariffs", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.ClearTariffsRequest{
				CustomData: customData21(),
				EVSEID:     int32Ptr21(1),
				TariffIds:  []string{"tariff-1"},
			},
			Valid: true,
		},
		{
			Name: "tariffIds item exceeds maxLength",
			Message: messages.ClearTariffsRequest{
				TariffIds: []string{strings.Repeat("x", 61)},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"evseId":     1,
				"tariffIds":  []string{"tariff-1"},
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"evseId":     1,
				"tariffIds":  []string{"tariff-1"},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearTariffs21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearTariffs", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.ClearTariffsResponse{
				ClearTariffsResult: []messages.ClearTariffsResultType{
					{
						CustomData: customData21(),
						Status:     "Accepted",
						StatusInfo: statusInfo21(),
						TariffID:   strPtr21("tariff-1"),
					},
				},
				CustomData: customData21(),
			},
			Valid: true,
		},
		{
			Name:    "missing clearTariffsResult",
			Message: map[string]any{"customData": customDataMap21()},
			Valid:   false,
		},
		{
			Name: "missing clearTariffsResult.status",
			Message: map[string]any{
				"clearTariffsResult": []map[string]any{{
					"customData": customDataMap21(),
					"statusInfo": statusInfoMap21(),
					"tariffId":   "tariff-1",
				}},
			},
			Valid: false,
		},
		{
			Name: "invalid clearTariffsResult.status enum",
			Message: messages.ClearTariffsResponse{
				ClearTariffsResult: []messages.ClearTariffsResultType{{Status: "InvalidStatus"}},
			},
			Valid: false,
		},
		{
			Name: "clearTariffsResult.tariffId exceeds maxLength",
			Message: messages.ClearTariffsResponse{
				ClearTariffsResult: []messages.ClearTariffsResultType{{
					Status:   "Accepted",
					TariffID: strPtr21(strings.Repeat("x", 61)),
				}},
			},
			Valid: false,
		},
		{
			Name: "missing clearTariffsResult.statusInfo.reasonCode",
			Message: map[string]any{
				"clearTariffsResult": []map[string]any{{
					"status":     "Accepted",
					"statusInfo": map[string]any{"additionalInfo": "details", "customData": customDataMap21()},
				}},
			},
			Valid: false,
		},
		{
			Name: "clearTariffsResult.statusInfo.reasonCode exceeds maxLength",
			Message: messages.ClearTariffsResponse{
				ClearTariffsResult: []messages.ClearTariffsResultType{{
					Status: "Accepted",
					StatusInfo: &messages.StatusInfoType{
						ReasonCode: strings.Repeat("x", 21),
					},
				}},
			},
			Valid: false,
		},
		{
			Name: "clearTariffsResult.statusInfo.additionalInfo exceeds maxLength",
			Message: messages.ClearTariffsResponse{
				ClearTariffsResult: []messages.ClearTariffsResultType{{
					Status: "Accepted",
					StatusInfo: &messages.StatusInfoType{
						AdditionalInfo: strPtr21(strings.Repeat("x", 1025)),
						ReasonCode:     "OK",
					},
				}},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"clearTariffsResult": []map[string]any{{"status": "Accepted"}},
				"customData":         map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"clearTariffsResult": []map[string]any{{"status": "Accepted"}},
				"customData":         map[string]any{"vendorId": strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearTariffs21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.ClearTariffs)
}
