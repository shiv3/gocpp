package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestHeartbeat21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "Heartbeat", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.HeartbeatRequest{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestHeartbeat21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "Heartbeat", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.HeartbeatResponse{
				CurrentTime: testTime(),
			},
			Valid: true,
		},
		{
			Name:    "missing currentTime",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestHeartbeat21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.Heartbeat)
}
