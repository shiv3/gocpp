package conf201e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestSetMonitoringBase201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetMonitoringBase", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid all request",
			Message: messages.SetMonitoringBaseRequest{
				MonitoringBase: "All",
			},
			Valid: true,
		},
		{
			Name: "valid factory default request",
			Message: messages.SetMonitoringBaseRequest{
				MonitoringBase: "FactoryDefault",
			},
			Valid: true,
		},
		{
			Name: "valid hard wired only request",
			Message: messages.SetMonitoringBaseRequest{
				MonitoringBase: "HardWiredOnly",
			},
			Valid: true,
		},
		{
			Name: "invalid monitoringBase enum",
			Message: messages.SetMonitoringBaseRequest{
				MonitoringBase: "invalidMonitoringBase",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing monitoringBase",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringBase201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetMonitoringBase", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SetMonitoringBaseResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SetMonitoringBaseResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.SetMonitoringBaseResponse{
				Status: "invalidDeviceModelStatus",
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

func TestSetMonitoringBase201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.SetMonitoringBase)
}
