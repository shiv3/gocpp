package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetCertificateStatus201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid ocsp request data",
			Message: messages.GetCertificateStatusRequest{
				OcspRequestData: ocspRequestData(),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing ocspRequestData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid hashAlgorithm enum",
			Message: messages.GetCertificateStatusRequest{
				OcspRequestData: messages.OCSPRequestDataType{
					HashAlgorithm:  "invalidHashAlgorithm",
					IssuerNameHash: "hash00",
					IssuerKeyHash:  "hash01",
					SerialNumber:   "serial0",
					ResponderURL:   "http://someUrl",
				},
			},
			Valid: false,
		},
	}

	runValidation201(t, "GetCertificateStatus", "request", cases)
}

func TestGetCertificateStatus201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid accepted response with ocspResult",
			Message: messages.GetCertificateStatusResponse{
				Status:     "Accepted",
				OcspResult: ptr("deadbeef"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.GetCertificateStatusResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid failed response",
			Message: messages.GetCertificateStatusResponse{
				Status: "Failed",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.GetCertificateStatusResponse{
				Status: "invalidGenericStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid ocspResult exceeds maxLength 5500",
			Message: messages.GetCertificateStatusResponse{
				Status:     "Accepted",
				OcspResult: ptr(longString(5501)),
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	runValidation201(t, "GetCertificateStatus", "response", cases)
}

func TestGetCertificateStatus201_Direction(t *testing.T) {
	assertCSMSRejectsWrongDirection(t, v201profiles.GetCertificateStatus)
}
