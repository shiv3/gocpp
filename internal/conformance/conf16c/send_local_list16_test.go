package conf16c

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestSendLocalList16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "SendLocalList", "request")

	expiryDate := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)
	parentIDTag := "000000"
	longIDTag := strings.Repeat("x", 21)
	localAuthEntry := messages.LocalAuthorizationList{
		IDTag: "12345",
		IDTagInfo: &messages.IDTagInfo{
			ExpiryDate:  &expiryDate,
			ParentIDTag: &parentIDTag,
			Status:      messages.IDTagInfoStatusAccepted,
		},
	}
	invalidAuthEntry := messages.LocalAuthorizationList{
		IDTag: "12345",
		IDTagInfo: &messages.IDTagInfo{
			ExpiryDate:  &expiryDate,
			ParentIDTag: &parentIDTag,
			Status:      messages.IDTagInfoStatus("invalidStatus"),
		},
	}
	cases := []conformance.ValidationCase{
		{
			Name: "valid differential with local authorization list",
			Message: messages.SendLocalListRequest{
				UpdateType:             messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion:            1,
				LocalAuthorizationList: []messages.LocalAuthorizationList{localAuthEntry},
			},
			Valid: true,
		},
		{
			Name: "valid empty local authorization list",
			Message: messages.SendLocalListRequest{
				UpdateType:             messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion:            1,
				LocalAuthorizationList: []messages.LocalAuthorizationList{},
			},
			Valid: true,
		},
		{
			Name: "valid without local authorization list",
			Message: messages.SendLocalListRequest{
				UpdateType:  messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion: 1,
			},
			Valid: true,
		},
		{
			Name: "valid zero listVersion",
			Message: messages.SendLocalListRequest{
				UpdateType:  messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion: 0,
			},
			Valid: true,
		},
		{
			Name: "invalid nested idTagInfo status enum",
			Message: messages.SendLocalListRequest{
				UpdateType:             messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion:            1,
				LocalAuthorizationList: []messages.LocalAuthorizationList{invalidAuthEntry},
			},
			Valid: false,
		},
		{
			Name: "invalid updateType enum",
			Message: messages.SendLocalListRequest{
				UpdateType:  messages.SendLocalListRequestUpdateType("invalidUpdateType"),
				ListVersion: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid idTag exceeds maxLength 20",
			Message: messages.SendLocalListRequest{
				UpdateType:  messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion: 1,
				LocalAuthorizationList: []messages.LocalAuthorizationList{
					{
						IDTag: longIDTag,
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid parentIdTag exceeds maxLength 20",
			Message: messages.SendLocalListRequest{
				UpdateType:  messages.SendLocalListRequestUpdateTypeDifferential,
				ListVersion: 1,
				LocalAuthorizationList: []messages.LocalAuthorizationList{
					{
						IDTag: "12345",
						IDTagInfo: &messages.IDTagInfo{
							ParentIDTag: &longIDTag,
							Status:      messages.IDTagInfoStatusAccepted,
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing updateType",
			Message: map[string]any{
				"listVersion": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing listVersion",
			Message: map[string]any{
				"updateType": messages.SendLocalListRequestUpdateTypeDifferential,
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		// TODO(parity): needs schema override for minimum:0 on listVersion.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSendLocalList16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "SendLocalList", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted status",
			Message: messages.SendLocalListResponse{
				Status: messages.SendLocalListResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.SendLocalListResponse{
				Status: messages.SendLocalListResponseStatus("invalidStatus"),
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

func TestSendLocalList16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.SendLocalListRequest, messages.SendLocalListResponse]{
		Action:    v16profiles.SendLocalList.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.SendLocalListRequest) (messages.SendLocalListResponse, error) {
		return messages.SendLocalListResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
