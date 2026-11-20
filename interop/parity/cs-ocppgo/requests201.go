// Code generated from the ocpp-go OCPP 2.0.1 CSMS send switch; do not edit by hand.

package main

import (
	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/authorization"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/availability"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/data"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/diagnostics"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/display"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/iso15118"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/localauth"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/provisioning"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/remotecontrol"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/reservation"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/security"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/tariffcost"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/transactions"
)

var requests201 = map[string]func() ocpp.Request{
	"CancelReservation":          func() ocpp.Request { return &reservation.CancelReservationRequest{} },
	"CertificateSigned":          func() ocpp.Request { return &security.CertificateSignedRequest{} },
	"ChangeAvailability":         func() ocpp.Request { return &availability.ChangeAvailabilityRequest{} },
	"ClearCache":                 func() ocpp.Request { return &authorization.ClearCacheRequest{} },
	"ClearChargingProfile":       func() ocpp.Request { return &smartcharging.ClearChargingProfileRequest{} },
	"ClearDisplayMessage":        func() ocpp.Request { return &display.ClearDisplayRequest{} },
	"ClearVariableMonitoring":    func() ocpp.Request { return &diagnostics.ClearVariableMonitoringRequest{} },
	"CostUpdated":                func() ocpp.Request { return &tariffcost.CostUpdatedRequest{} },
	"CustomerInformation":        func() ocpp.Request { return &diagnostics.CustomerInformationRequest{} },
	"DataTransfer":               func() ocpp.Request { return &data.DataTransferRequest{} },
	"DeleteCertificate":          func() ocpp.Request { return &iso15118.DeleteCertificateRequest{} },
	"GetBaseReport":              func() ocpp.Request { return &provisioning.GetBaseReportRequest{} },
	"GetChargingProfiles":        func() ocpp.Request { return &smartcharging.GetChargingProfilesRequest{} },
	"GetCompositeSchedule":       func() ocpp.Request { return &smartcharging.GetCompositeScheduleRequest{} },
	"GetDisplayMessages":         func() ocpp.Request { return &display.GetDisplayMessagesRequest{} },
	"GetInstalledCertificateIds": func() ocpp.Request { return &iso15118.GetInstalledCertificateIdsRequest{} },
	"GetLocalListVersion":        func() ocpp.Request { return &localauth.GetLocalListVersionRequest{} },
	"GetLog":                     func() ocpp.Request { return &diagnostics.GetLogRequest{} },
	"GetMonitoringReport":        func() ocpp.Request { return &diagnostics.GetMonitoringReportRequest{} },
	"GetReport":                  func() ocpp.Request { return &provisioning.GetReportRequest{} },
	"GetTransactionStatus":       func() ocpp.Request { return &transactions.GetTransactionStatusRequest{} },
	"GetVariables":               func() ocpp.Request { return &provisioning.GetVariablesRequest{} },
	"InstallCertificate":         func() ocpp.Request { return &iso15118.InstallCertificateRequest{} },
	"PublishFirmware":            func() ocpp.Request { return &firmware.PublishFirmwareRequest{} },
	"RequestStartTransaction":    func() ocpp.Request { return &remotecontrol.RequestStartTransactionRequest{} },
	"RequestStopTransaction":     func() ocpp.Request { return &remotecontrol.RequestStopTransactionRequest{} },
	"ReserveNow":                 func() ocpp.Request { return &reservation.ReserveNowRequest{} },
	"Reset":                      func() ocpp.Request { return &provisioning.ResetRequest{} },
	"SendLocalList":              func() ocpp.Request { return &localauth.SendLocalListRequest{} },
	"SetChargingProfile":         func() ocpp.Request { return &smartcharging.SetChargingProfileRequest{} },
	"SetDisplayMessage":          func() ocpp.Request { return &display.SetDisplayMessageRequest{} },
	"SetMonitoringBase":          func() ocpp.Request { return &diagnostics.SetMonitoringBaseRequest{} },
	"SetMonitoringLevel":         func() ocpp.Request { return &diagnostics.SetMonitoringLevelRequest{} },
	"SetNetworkProfile":          func() ocpp.Request { return &provisioning.SetNetworkProfileRequest{} },
	"SetVariableMonitoring":      func() ocpp.Request { return &diagnostics.SetVariableMonitoringRequest{} },
	"SetVariables":               func() ocpp.Request { return &provisioning.SetVariablesRequest{} },
	"TriggerMessage":             func() ocpp.Request { return &remotecontrol.TriggerMessageRequest{} },
	"UnlockConnector":            func() ocpp.Request { return &remotecontrol.UnlockConnectorRequest{} },
	"UnpublishFirmware":          func() ocpp.Request { return &firmware.UnpublishFirmwareRequest{} },
	"UpdateFirmware":             func() ocpp.Request { return &firmware.UpdateFirmwareRequest{} },
}
