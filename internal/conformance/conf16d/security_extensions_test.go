package conf16d_test

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
)

func TestCertificateSigned16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "CertificateSigned", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.CertificateSignedRequest{
				CertificateChain: "sampleCert",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing certificateChain",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid certificateChain exceeds maxLength 10000",
			Message: messages.CertificateSignedRequest{
				CertificateChain: strings.Repeat("x", 10001),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCertificateSigned16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "CertificateSigned", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.CertificateSignedResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.CertificateSignedResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.CertificateSignedResponse{
				Status: "invalidCertificateSignedStatus",
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

func TestCertificateSigned16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.CertificateSigned)
}

func TestDeleteCertificate16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "DeleteCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: testCertificateHashData(),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing certificateHashData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown hashAlgorithm enum",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "invalidHashAlgorithm",
					IssuerKeyHash:  "hash01",
					IssuerNameHash: "hash00",
					SerialNumber:   "serial0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing hashAlgorithm",
			Message: map[string]any{
				"certificateHashData": map[string]any{
					"issuerNameHash": "hash00",
					"issuerKeyHash":  "hash01",
					"serialNumber":   "serial0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing issuerNameHash",
			Message: map[string]any{
				"certificateHashData": map[string]any{
					"hashAlgorithm": "SHA256",
					"issuerKeyHash": "hash01",
					"serialNumber":  "serial0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing issuerKeyHash",
			Message: map[string]any{
				"certificateHashData": map[string]any{
					"hashAlgorithm":  "SHA256",
					"issuerNameHash": "hash00",
					"serialNumber":   "serial0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing serialNumber",
			Message: map[string]any{
				"certificateHashData": map[string]any{
					"hashAlgorithm":  "SHA256",
					"issuerNameHash": "hash00",
					"issuerKeyHash":  "hash01",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid issuerNameHash exceeds maxLength 128",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  "hash01",
					IssuerNameHash: strings.Repeat("x", 129),
					SerialNumber:   "serial0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid issuerKeyHash exceeds maxLength 128",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  strings.Repeat("x", 129),
					IssuerNameHash: "hash00",
					SerialNumber:   "serial0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid serialNumber exceeds maxLength 40",
			Message: messages.DeleteCertificateRequest{
				CertificateHashData: messages.CertificateHashDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  "hash01",
					IssuerNameHash: "hash00",
					SerialNumber:   strings.Repeat("x", 41),
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDeleteCertificate16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "DeleteCertificate", "response")

	cases := []conformance.ValidationCase{
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
			Name: "invalid unknown status enum",
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

	conformance.RunValidationTable(t, validator, cases)
}

func TestDeleteCertificate16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.DeleteCertificate)
}

func TestGetInstalledCertificateIds16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "GetInstalledCertificateIds", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid central system root certificate request",
			Message: messages.GetInstalledCertificateIdsRequest{
				CertificateType: "CentralSystemRootCertificate",
			},
			Valid: true,
		},
		{
			Name: "valid manufacturer root certificate request",
			Message: messages.GetInstalledCertificateIdsRequest{
				CertificateType: "ManufacturerRootCertificate",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing certificateType",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown certificateType enum",
			Message: messages.GetInstalledCertificateIdsRequest{
				CertificateType: "invalidCertificateUse",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetInstalledCertificateIds16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "GetInstalledCertificateIds", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.GetInstalledCertificateIdsResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid accepted response with certificate hash data",
			Message: messages.GetInstalledCertificateIdsResponse{
				CertificateHashData: []messages.CertificateHashDataType{testCertificateHashData()},
				Status:              "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid not found response",
			Message: messages.GetInstalledCertificateIdsResponse{
				Status: "NotFound",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.GetInstalledCertificateIdsResponse{
				Status: "invalidGetInstalledCertificateStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid certificateHashData unknown hashAlgorithm enum",
			Message: messages.GetInstalledCertificateIdsResponse{
				CertificateHashData: []messages.CertificateHashDataType{
					{
						HashAlgorithm:  "invalidHashAlgorithm",
						IssuerKeyHash:  "hash01",
						IssuerNameHash: "hash00",
						SerialNumber:   "serial0",
					},
				},
				Status: "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid certificateHashData serialNumber exceeds maxLength 40",
			Message: messages.GetInstalledCertificateIdsResponse{
				CertificateHashData: []messages.CertificateHashDataType{
					{
						HashAlgorithm:  "SHA256",
						IssuerKeyHash:  "hash01",
						IssuerNameHash: "hash00",
						SerialNumber:   strings.Repeat("x", 41),
					},
				},
				Status: "Accepted",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetInstalledCertificateIds16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.GetInstalledCertificateIds)
}

func TestInstallCertificate16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "InstallCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid manufacturer root certificate request",
			Message: messages.InstallCertificateRequest{
				Certificate:     "0xdeadbeef",
				CertificateType: "ManufacturerRootCertificate",
			},
			Valid: true,
		},
		{
			Name: "valid central system root certificate request",
			Message: messages.InstallCertificateRequest{
				Certificate:     "0xdeadbeef",
				CertificateType: "CentralSystemRootCertificate",
			},
			Valid: true,
		},
		{
			Name: "invalid missing certificate",
			Message: map[string]any{
				"certificateType": "ManufacturerRootCertificate",
			},
			Valid: false,
		},
		{
			Name: "invalid missing certificateType",
			Message: map[string]any{
				"certificate": "0xdeadbeef",
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown certificateType enum",
			Message: messages.InstallCertificateRequest{
				Certificate:     "0xdeadbeef",
				CertificateType: "invalidCertificateUse",
			},
			Valid: false,
		},
		{
			Name: "invalid certificate exceeds maxLength 5500",
			Message: messages.InstallCertificateRequest{
				Certificate:     strings.Repeat("x", 5501),
				CertificateType: "ManufacturerRootCertificate",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestInstallCertificate16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "InstallCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.InstallCertificateResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.InstallCertificateResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid failed response",
			Message: messages.InstallCertificateResponse{
				Status: "Failed",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.InstallCertificateResponse{
				Status: "invalidInstallCertificateStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestInstallCertificate16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.InstallCertificate)
}

func TestSignCertificate16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "SignCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.SignCertificateRequest{
				Csr: "deadc0de",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing csr",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid csr exceeds maxLength 5500",
			Message: messages.SignCertificateRequest{
				Csr: strings.Repeat("x", 5501),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignCertificate16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "SignCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.SignCertificateResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.SignCertificateResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.SignCertificateResponse{
				Status: "invalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignCertificate16_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v16profiles.SignCertificate)
}

func TestSignedUpdateFirmware16_RequestValidation(t *testing.T) {
	validator := must16Validator(t, "SignedUpdateFirmware", "request")

	firmware := testFirmware()
	minimalFirmware := testFirmware()
	minimalFirmware.InstallDateTime = nil

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware:      firmware,
				RequestID:     42,
				Retries:       int32Ptr(5),
				RetryInterval: int32Ptr(300),
			},
			Valid: true,
		},
		{
			Name: "valid without retryInterval",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware:  firmware,
				RequestID: 42,
				Retries:   int32Ptr(5),
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware:  minimalFirmware,
				RequestID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid requestId zero",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware:  firmware,
				RequestID: 0,
			},
			Valid: true,
		},
		{
			Name: "valid zero retries and retryInterval",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware:      firmware,
				RequestID:     42,
				Retries:       int32Ptr(0),
				RetryInterval: int32Ptr(0),
			},
			Valid: true,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"firmware": map[string]any{
					"location":           "https://someurl",
					"retrieveDateTime":   testTime(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing firmware",
			Message: map[string]any{
				"requestId": 42,
			},
			Valid: false,
		},
		{
			Name: "invalid firmware missing location",
			Message: map[string]any{
				"requestId": 42,
				"firmware": map[string]any{
					"retrieveDateTime":   testTime(),
					"installDateTime":    testTime(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid firmware missing retrieveDateTime",
			Message: map[string]any{
				"requestId": 42,
				"firmware": map[string]any{
					"location":           "https://someurl",
					"installDateTime":    testTime(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid firmware missing signingCertificate",
			Message: map[string]any{
				"requestId": 42,
				"firmware": map[string]any{
					"location":         "https://someurl",
					"retrieveDateTime": testTime(),
					"installDateTime":  testTime(),
					"signature":        "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid firmware missing signature",
			Message: map[string]any{
				"requestId": 42,
				"firmware": map[string]any{
					"location":           "https://someurl",
					"retrieveDateTime":   testTime(),
					"installDateTime":    testTime(),
					"signingCertificate": "1337c0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid firmware location exceeds maxLength 512",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware: messages.FirmwareType{
					InstallDateTime:    timePtr(testTime()),
					Location:           strings.Repeat("x", 513),
					RetrieveDateTime:   testTime(),
					Signature:          "deadc0de",
					SigningCertificate: "1337c0de",
				},
				RequestID: 42,
			},
			Valid: false,
		},
		{
			Name: "invalid firmware signingCertificate exceeds maxLength 5500",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware: messages.FirmwareType{
					InstallDateTime:    timePtr(testTime()),
					Location:           "https://someurl",
					RetrieveDateTime:   testTime(),
					Signature:          "deadc0de",
					SigningCertificate: strings.Repeat("x", 5501),
				},
				RequestID: 42,
			},
			Valid: false,
		},
		{
			Name: "invalid firmware signature exceeds maxLength 800",
			Message: messages.SignedUpdateFirmwareRequest{
				Firmware: messages.FirmwareType{
					InstallDateTime:    timePtr(testTime()),
					Location:           "https://someurl",
					RetrieveDateTime:   testTime(),
					Signature:          strings.Repeat("x", 801),
					SigningCertificate: "1337c0de",
				},
				RequestID: 42,
			},
			Valid: false,
		},
		{
			Name: "invalid requestId below minimum",
			Message: map[string]any{
				"requestId": -1,
				"firmware": map[string]any{
					"location":           "https://someurl",
					"retrieveDateTime":   testTime(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid retries below minimum",
			Message: map[string]any{
				"requestId": 42,
				"retries":   -1,
				"firmware": map[string]any{
					"location":           "https://someurl",
					"retrieveDateTime":   testTime(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid retryInterval below minimum",
			Message: map[string]any{
				"requestId":     42,
				"retryInterval": -1,
				"firmware": map[string]any{
					"location":           "https://someurl",
					"retrieveDateTime":   testTime(),
					"signingCertificate": "1337c0de",
					"signature":          "deadc0de",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignedUpdateFirmware16_ResponseValidation(t *testing.T) {
	validator := must16Validator(t, "SignedUpdateFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.SignedUpdateFirmwareResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.SignedUpdateFirmwareResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid accepted canceled response",
			Message: messages.SignedUpdateFirmwareResponse{
				Status: "AcceptedCanceled",
			},
			Valid: true,
		},
		{
			Name: "valid invalid certificate response",
			Message: messages.SignedUpdateFirmwareResponse{
				Status: "InvalidCertificate",
			},
			Valid: true,
		},
		{
			Name: "valid revoked certificate response",
			Message: messages.SignedUpdateFirmwareResponse{
				Status: "RevokedCertificate",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.SignedUpdateFirmwareResponse{
				Status: "invalidFirmwareUpdateStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignedUpdateFirmware16_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v16profiles.SignedUpdateFirmware)
}
