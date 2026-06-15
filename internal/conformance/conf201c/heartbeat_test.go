package conf201c

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestHeartbeat201_RequestValidation(t *testing.T) {
	validator := validator201(t, "Heartbeat", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty request",
			Message: messages.HeartbeatRequest{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestHeartbeat201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "Heartbeat", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid currentTime",
			Message: messages.HeartbeatResponse{
				CurrentTime: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing currentTime",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestHeartbeat201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.Heartbeat)
}
