package conf201a

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	"github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestAuthorize201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "Authorize", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.AuthorizeRequest{
				IDToken: messages.IdTokenType{IDToken: "1234", Type: "KeyCode"},
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.AuthorizeRequest{
				Certificate: strPtr("deadc0de"),
				CustomData:  testCustomData(),
				IDToken: messages.IdTokenType{
					AdditionalInfo: []messages.AdditionalInfoType{
						{
							AdditionalIDToken: "0000",
							CustomData:        testCustomData(),
							Type:              "someType",
						},
					},
					CustomData: testCustomData(),
					IDToken:    "1234",
					Type:       "KeyCode",
				},
				Iso15118CertificateHashData: []messages.OCSPRequestDataType{
					{
						CustomData:     testCustomData(),
						HashAlgorithm:  "SHA256",
						IssuerKeyHash:  "hash1",
						IssuerNameHash: "hash0",
						ResponderURL:   "https://example.com/ocsp",
						SerialNumber:   "serial0",
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "invalid missing idToken",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing idToken.idToken",
			Message: map[string]any{
				"idToken": map[string]any{
					"type": "KeyCode",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing idToken.type",
			Message: map[string]any{
				"idToken": map[string]any{
					"idToken": "1234",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid idToken.type enum",
			Message: messages.AuthorizeRequest{
				IDToken: messages.IdTokenType{IDToken: "1234", Type: "NotAStatus"},
			},
			Valid: false,
		},
		{
			Name: "invalid hashAlgorithm enum",
			Message: messages.AuthorizeRequest{
				IDToken: messages.IdTokenType{IDToken: "1234", Type: "KeyCode"},
				Iso15118CertificateHashData: []messages.OCSPRequestDataType{
					{
						HashAlgorithm:  "NotAStatus",
						IssuerKeyHash:  "hash1",
						IssuerNameHash: "hash0",
						ResponderURL:   "https://example.com/ocsp",
						SerialNumber:   "serial0",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid certificate exceeds maxLength 5500",
			Message: messages.AuthorizeRequest{
				Certificate: strPtr(strings.Repeat("x", 5501)),
				IDToken:     messages.IdTokenType{IDToken: "1234", Type: "KeyCode"},
			},
			Valid: false,
		},
		{
			Name: "invalid idToken.idToken exceeds maxLength 36",
			Message: messages.AuthorizeRequest{
				IDToken: messages.IdTokenType{IDToken: strings.Repeat("x", 37), Type: "KeyCode"},
			},
			Valid: false,
		},
		{
			Name: "invalid additionalIdToken exceeds maxLength 36",
			Message: messages.AuthorizeRequest{
				IDToken: messages.IdTokenType{
					AdditionalInfo: []messages.AdditionalInfoType{
						{AdditionalIDToken: strings.Repeat("x", 37), Type: "someType"},
					},
					IDToken: "1234",
					Type:    "KeyCode",
				},
			},
			Valid: false,
		},
		// TODO(parity): empty optional array — OCA schema has no minItems, so it is
		// valid; ocpp-go enforced min=1 via struct tag. Needs a schema override to assert.
		{
			Name: "invalid iso15118CertificateHashData exceeds maxItems 4",
			Message: messages.AuthorizeRequest{
				IDToken: messages.IdTokenType{IDToken: "1234", Type: "KeyCode"},
				Iso15118CertificateHashData: []messages.OCSPRequestDataType{
					{HashAlgorithm: "SHA256", IssuerKeyHash: "hash1", IssuerNameHash: "hash0", ResponderURL: "https://example.com/0", SerialNumber: "serial0"},
					{HashAlgorithm: "SHA256", IssuerKeyHash: "hash1", IssuerNameHash: "hash0", ResponderURL: "https://example.com/1", SerialNumber: "serial1"},
					{HashAlgorithm: "SHA256", IssuerKeyHash: "hash1", IssuerNameHash: "hash0", ResponderURL: "https://example.com/2", SerialNumber: "serial2"},
					{HashAlgorithm: "SHA256", IssuerKeyHash: "hash1", IssuerNameHash: "hash0", ResponderURL: "https://example.com/3", SerialNumber: "serial3"},
					{HashAlgorithm: "SHA256", IssuerKeyHash: "hash1", IssuerNameHash: "hash0", ResponderURL: "https://example.com/4", SerialNumber: "serial4"},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestAuthorize201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "Authorize", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal response",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{Status: "Accepted"},
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.AuthorizeResponse{
				CertificateStatus: strPtr("Accepted"),
				CustomData:        testCustomData(),
				IDTokenInfo: messages.IdTokenInfoType{
					CacheExpiryDateTime: &[]time.Time{testTime()}[0],
					ChargingPriority:    int32Ptr(1),
					CustomData:          testCustomData(),
					EVSEID:              []int32{1},
					GroupIDToken: &messages.IdTokenType{
						IDToken: "group",
						Type:    "Local",
					},
					Language1: strPtr("en"),
					Language2: strPtr("ja"),
					PersonalMessage: &messages.MessageContentType{
						Content:    "Welcome",
						CustomData: testCustomData(),
						Format:     "UTF8",
						Language:   strPtr("en"),
					},
					Status: "Accepted",
				},
			},
			Valid: true,
		},
		{
			Name:    "invalid missing idTokenInfo",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing idTokenInfo.status",
			Message: map[string]any{
				"idTokenInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid certificateStatus enum",
			Message: messages.AuthorizeResponse{
				CertificateStatus: strPtr("NotAStatus"),
				IDTokenInfo:       messages.IdTokenInfoType{Status: "Accepted"},
			},
			Valid: false,
		},
		{
			Name: "invalid idTokenInfo.status enum",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{Status: "NotAStatus"},
			},
			Valid: false,
		},
		{
			Name: "invalid language1 exceeds maxLength 8",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{
					Language1: strPtr(strings.Repeat("x", 9)),
					Status:    "Accepted",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid personalMessage.content exceeds maxLength 512",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{
					PersonalMessage: &messages.MessageContentType{
						Content: strings.Repeat("x", 513),
						Format:  "UTF8",
					},
					Status: "Accepted",
				},
			},
			Valid: false,
		},
		// TODO(parity): empty optional array — OCA schema has no minItems, so it is
		// valid; ocpp-go enforced min=1 via struct tag. Needs a schema override to assert.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestAuthorize201_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.AuthorizeRequest, messages.AuthorizeResponse]{
		Action:    profiles.Authorize.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.AuthorizeRequest) (messages.AuthorizeResponse, error) {
		return messages.AuthorizeResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
