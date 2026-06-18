package conf201a

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestClearVariableMonitoring201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearVariableMonitoring", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid multiple ids",
			Message: messages.ClearVariableMonitoringRequest{
				ID: []int32{0, 2, 15},
			},
			Valid: true,
		},
		{
			Name: "valid single id",
			Message: messages.ClearVariableMonitoringRequest{
				ID: []int32{0},
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.ClearVariableMonitoringRequest{
				CustomData: testCustomData(),
				ID:         []int32{1, 2},
			},
			Valid: true,
		},
		{
			Name: "invalid empty id list",
			Message: messages.ClearVariableMonitoringRequest{
				ID: []int32{},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing id",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid id entry below minimum",
			Message: map[string]any{
				"id": []any{-1},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearVariableMonitoring201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearVariableMonitoring", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted result",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						ID:     2,
						Status: "Accepted",
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						CustomData: testCustomData(),
						ID:         2,
						Status:     "NotFound",
						StatusInfo: testStatusInfo(),
					},
				},
				CustomData: testCustomData(),
			},
			Valid: true,
		},
		{
			Name: "invalid result missing status",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{ID: 2},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty result list",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing clearMonitoringResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						ID:     2,
						Status: "BadEnum",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid result id below minimum",
			Message: map[string]any{
				"clearMonitoringResult": []any{
					map[string]any{
						"id":     -1,
						"status": "Accepted",
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearVariableMonitoring201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.ClearVariableMonitoringRequest, messages.ClearVariableMonitoringResponse]{
		Action:    v201profiles.ClearVariableMonitoring.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ClearVariableMonitoringRequest) (messages.ClearVariableMonitoringResponse, error) {
		return messages.ClearVariableMonitoringResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
