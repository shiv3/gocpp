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

func TestHeartbeat21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
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
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
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
