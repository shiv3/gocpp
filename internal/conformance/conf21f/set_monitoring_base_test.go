package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSetMonitoringBase21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "SetMonitoringBase", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetMonitoringBaseRequest{
				MonitoringBase: "All",
			},
			Valid: true,
		},
		{
			Name:    "missing monitoringBase",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.SetMonitoringBaseRequest{
				CustomData:     &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				MonitoringBase: "All",
			},
			Valid: false,
		},
		{
			Name: "invalid enum monitoringBase",
			Message: messages.SetMonitoringBaseRequest{
				MonitoringBase: "InvalidBase",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringBase21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "SetMonitoringBase", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetMonitoringBaseResponse{
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
			Message: messages.SetMonitoringBaseResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.SetMonitoringBaseResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringBase21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.SetMonitoringBase)
}
