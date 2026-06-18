package conf16a

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestAuthorize16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "Authorize", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.AuthorizeRequest{
				IDTag: "12345",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing idTag",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid idTag exceeds maxLength 20",
			Message: messages.AuthorizeRequest{
				IDTag: strings.Repeat("x", 21),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestAuthorize16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "Authorize", "response")

	expiry := time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)
	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.AuthorizeResponse{
				IDTagInfo: messages.IDTagInfo{
					ExpiryDate:  timePtr(expiry),
					ParentIDTag: strPtr("00000"),
					Status:      messages.IDTagInfoStatusAccepted,
				},
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.AuthorizeResponse{
				IDTagInfo: messages.IDTagInfo{
					Status: messages.IDTagInfoStatus("invalidAuthorizationStatus"),
				},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing idTagInfo",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestAuthorize16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.AuthorizeRequest, messages.AuthorizeResponse]{
		Action:    v16profiles.Authorize.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.AuthorizeRequest) (messages.AuthorizeResponse, error) {
		return messages.AuthorizeResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func strPtr(s string) *string {
	return &s
}

func timePtr(t time.Time) *time.Time {
	return &t
}
