package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyReport21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyReportRequest{
				GeneratedAt: fixedTime21(),
				ReportData:  []messages.ReportDataType{testReportData21()},
				RequestID:   1,
				SeqNo:       0,
			},
			Valid: true,
		},
		{
			Name: "missing generatedAt",
			Message: map[string]any{
				"requestId": 1,
				"seqNo":     0,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength reportData.variableAttribute.value",
			Message: messages.NotifyReportRequest{
				GeneratedAt: fixedTime21(),
				ReportData: []messages.ReportDataType{
					{
						Component: testComponent21(),
						Variable:  testVariable21(),
						VariableAttribute: []messages.VariableAttributeType{
							{
								Value: stringPtr21(strings.Repeat("x", 2501)),
							},
						},
					},
				},
				RequestID: 1,
				SeqNo:     0,
			},
			Valid: false,
		},
		{
			Name: "invalid enum reportData.variableAttribute.type",
			Message: messages.NotifyReportRequest{
				GeneratedAt: fixedTime21(),
				ReportData: []messages.ReportDataType{
					{
						Component: testComponent21(),
						Variable:  testVariable21(),
						VariableAttribute: []messages.VariableAttributeType{
							{
								Type: stringPtr21("InvalidAttribute"),
							},
						},
					},
				},
				RequestID: 1,
				SeqNo:     0,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyReport21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyReportResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.NotifyReportResponse{
				CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyReport21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifyReport)
}
