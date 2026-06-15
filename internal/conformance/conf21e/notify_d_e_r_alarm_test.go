package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestNotifyDERAlarm21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyDERAlarm", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyDERAlarmRequest{
				ControlType: "FreqWatt",
				Timestamp:   testTime(),
			},
			Valid: true,
		},
		{
			Name: "missing controlType",
			Message: map[string]any{
				"timestamp": testTime(),
			},
			Valid: false,
		},
		{
			Name: "missing timestamp",
			Message: map[string]any{
				"controlType": "FreqWatt",
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength extraInfo",
			Message: messages.NotifyDERAlarmRequest{
				ControlType: "FreqWatt",
				ExtraInfo:   stringPtr(longString(201)),
				Timestamp:   testTime(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum controlType",
			Message: messages.NotifyDERAlarmRequest{
				ControlType: "invalidDERControl",
				Timestamp:   testTime(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDERAlarm21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyDERAlarm", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyDERAlarmResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDERAlarm21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.NotifyDERAlarm)
}
