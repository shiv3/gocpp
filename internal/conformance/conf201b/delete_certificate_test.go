package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestDeleteCertificate201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid certificate hash data",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: certificateHashData(),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing certificateHashData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid hashAlgorithm enum",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "invalidHashAlgorithm",
					IssuerNameHash: "hash00",
					IssuerKeyHash:  "hash01",
					SerialNumber:   "serial0",
				},
			},
			Valid: false,
		},
	}

	runValidation201(t, "DeleteCertificate", "request", cases)
}

func TestDeleteCertificate201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid accepted response",
			Message: messages.DeleteCertificateResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid failed response",
			Message: messages.DeleteCertificateResponse{
				Status: "Failed",
			},
			Valid: true,
		},
		{
			Name: "valid not found response",
			Message: messages.DeleteCertificateResponse{
				Status: "NotFound",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.DeleteCertificateResponse{
				Status: "invalidDeleteCertificateStatus",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	runValidation201(t, "DeleteCertificate", "response", cases)
}

func TestDeleteCertificate201_Direction(t *testing.T) {
	assertCPRejectsWrongDirection(t, v201profiles.DeleteCertificate)
}
