package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestDeleteCertificate21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "DeleteCertificate", "request")

	validCertificateHashData := map[string]any{
		"customData":     customDataMap21(),
		"hashAlgorithm":  "SHA256",
		"issuerKeyHash":  "issuer-key-hash",
		"issuerNameHash": "issuer-name-hash",
		"serialNumber":   "serial",
	}

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: certificateHashData21(),
				CustomData:          customData21(),
			},
			Valid: true,
		},
		{
			Name:    "missing certificateHashData",
			Message: map[string]any{"customData": customDataMap21()},
			Valid:   false,
		},
		{
			Name: "missing certificateHashData.hashAlgorithm",
			Message: map[string]any{
				"certificateHashData": without21(validCertificateHashData, "hashAlgorithm"),
			},
			Valid: false,
		},
		{
			Name: "missing certificateHashData.issuerNameHash",
			Message: map[string]any{
				"certificateHashData": without21(validCertificateHashData, "issuerNameHash"),
			},
			Valid: false,
		},
		{
			Name: "missing certificateHashData.issuerKeyHash",
			Message: map[string]any{
				"certificateHashData": without21(validCertificateHashData, "issuerKeyHash"),
			},
			Valid: false,
		},
		{
			Name: "missing certificateHashData.serialNumber",
			Message: map[string]any{
				"certificateHashData": without21(validCertificateHashData, "serialNumber"),
			},
			Valid: false,
		},
		{
			Name: "invalid certificateHashData.hashAlgorithm enum",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "InvalidHash",
					IssuerKeyHash:  "issuer-key-hash",
					IssuerNameHash: "issuer-name-hash",
					SerialNumber:   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "certificateHashData.issuerNameHash exceeds maxLength",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  "issuer-key-hash",
					IssuerNameHash: strings.Repeat("x", 129),
					SerialNumber:   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "certificateHashData.issuerKeyHash exceeds maxLength",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  strings.Repeat("x", 129),
					IssuerNameHash: "issuer-name-hash",
					SerialNumber:   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "certificateHashData.serialNumber exceeds maxLength",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  "issuer-key-hash",
					IssuerNameHash: "issuer-name-hash",
					SerialNumber:   strings.Repeat("x", 41),
				},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"certificateHashData": validCertificateHashData,
				"customData":          map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"certificateHashData": validCertificateHashData,
				"customData":          map[string]any{"vendorId": strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDeleteCertificate21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "DeleteCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.DeleteCertificateResponse{
				CustomData: customData21(),
				Status:     "Accepted",
				StatusInfo: statusInfo21(),
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{"customData": customDataMap21(), "statusInfo": statusInfoMap21()},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.DeleteCertificateResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
		{
			Name: "missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"additionalInfo": "details", "customData": customDataMap21()},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.reasonCode exceeds maxLength",
			Message: messages.DeleteCertificateResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.additionalInfo exceeds maxLength",
			Message: messages.DeleteCertificateResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: strPtr21(strings.Repeat("x", 1025)),
					ReasonCode:     "OK",
				},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"status":     "Accepted",
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"status":     "Accepted",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDeleteCertificate21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.DeleteCertificate)
}
