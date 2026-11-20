package main

import (
	"errors"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/localauth"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/reservation"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/parity/common"
)

func setup16(rec *common.Recorder, wsServer *ws.Server, dispatcher ocppj.ServerDispatcher, hk hooks) (csAPI, map[string]func() ocpp.Request) {
	endpoint := ocppj.NewServer(wsServer, dispatcher, nil,
		core.Profile, localauth.Profile, firmware.Profile, reservation.Profile,
		remotetrigger.Profile, smartcharging.Profile)
	cs := ocpp16.NewCentralSystem(endpoint, wsServer)
	h := &handler{rec: rec}
	cs.SetCoreHandler(h)
	cs.SetFirmwareManagementHandler(h)
	cs.SetLocalAuthListHandler(emptyHandler{})
	cs.SetReservationHandler(emptyHandler{})
	cs.SetRemoteTriggerHandler(emptyHandler{})
	cs.SetSmartChargingHandler(emptyHandler{})
	cs.SetNewChargingStationValidationHandler(hk.validate)
	cs.SetNewChargePointHandler(func(cp ocpp16.ChargePointConnection) { hk.connect(cp.ID()) })
	cs.SetChargePointDisconnectedHandler(func(cp ocpp16.ChargePointConnection) { hk.disconnect(cp.ID()) })
	return cs, requests16
}

// ---------------------------------------------------------------- handlers

type emptyHandler struct{}

type handler struct {
	rec *common.Recorder
	tx  common.TxCounter
}

func (h *handler) OnBootNotification(id string, req *core.BootNotificationRequest) (*core.BootNotificationConfirmation, error) {
	h.rec.Request(id, "BootNotification", req)
	resp := core.NewBootNotificationConfirmation(types.NewDateTime(common.FixedTimeT), common.BootInterval, core.RegistrationStatusAccepted)
	h.rec.Response(id, "BootNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnHeartbeat(id string, req *core.HeartbeatRequest) (*core.HeartbeatConfirmation, error) {
	h.rec.Request(id, "Heartbeat", req)
	resp := core.NewHeartbeatConfirmation(types.NewDateTime(common.FixedTimeT))
	h.rec.Response(id, "Heartbeat", resp, nil)
	return resp, nil
}

func idTagInfo(idTag string) *types.IdTagInfo {
	return types.NewIdTagInfo(types.AuthorizationStatus(common.AuthStatus(idTag)))
}

func (h *handler) OnAuthorize(id string, req *core.AuthorizeRequest) (*core.AuthorizeConfirmation, error) {
	h.rec.Request(id, "Authorize", req)
	resp := core.NewAuthorizationConfirmation(idTagInfo(req.IdTag))
	h.rec.Response(id, "Authorize", resp, nil)
	return resp, nil
}

func (h *handler) OnStartTransaction(id string, req *core.StartTransactionRequest) (*core.StartTransactionConfirmation, error) {
	h.rec.Request(id, "StartTransaction", req)
	resp := core.NewStartTransactionConfirmation(idTagInfo(req.IdTag), int(h.tx.Next()))
	h.rec.Response(id, "StartTransaction", resp, nil)
	return resp, nil
}

func (h *handler) OnStopTransaction(id string, req *core.StopTransactionRequest) (*core.StopTransactionConfirmation, error) {
	h.rec.Request(id, "StopTransaction", req)
	resp := core.NewStopTransactionConfirmation()
	if req.IdTag != "" {
		resp.IdTagInfo = types.NewIdTagInfo(types.AuthorizationStatusAccepted)
	}
	h.rec.Response(id, "StopTransaction", resp, nil)
	return resp, nil
}

func (h *handler) OnStatusNotification(id string, req *core.StatusNotificationRequest) (*core.StatusNotificationConfirmation, error) {
	h.rec.Request(id, "StatusNotification", req)
	resp := core.NewStatusNotificationConfirmation()
	h.rec.Response(id, "StatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnMeterValues(id string, req *core.MeterValuesRequest) (*core.MeterValuesConfirmation, error) {
	h.rec.Request(id, "MeterValues", req)
	resp := core.NewMeterValuesConfirmation()
	h.rec.Response(id, "MeterValues", resp, nil)
	return resp, nil
}

func (h *handler) OnDiagnosticsStatusNotification(id string, req *firmware.DiagnosticsStatusNotificationRequest) (*firmware.DiagnosticsStatusNotificationConfirmation, error) {
	h.rec.Request(id, "DiagnosticsStatusNotification", req)
	resp := firmware.NewDiagnosticsStatusNotificationConfirmation()
	h.rec.Response(id, "DiagnosticsStatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnFirmwareStatusNotification(id string, req *firmware.FirmwareStatusNotificationRequest) (*firmware.FirmwareStatusNotificationConfirmation, error) {
	h.rec.Request(id, "FirmwareStatusNotification", req)
	resp := firmware.NewFirmwareStatusNotificationConfirmation()
	h.rec.Response(id, "FirmwareStatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnDataTransfer(id string, req *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	h.rec.Request(id, "DataTransfer", req)
	d := common.DecideDataTransfer(req.VendorId, req.Data)
	switch d.Kind {
	case common.DTPanic:
		h.rec.PanicPending(id, "DataTransfer")
		panic(common.PanicMessage)
	case common.DTError:
		err := errors.New(common.ErrorMessage)
		h.rec.Response(id, "DataTransfer", nil, err)
		return nil, err
	}
	resp := core.NewDataTransferConfirmation(core.DataTransferStatusAccepted)
	switch d.Kind {
	case common.DTReject:
		resp.Status = core.DataTransferStatusRejected
	case common.DTSleep:
		time.Sleep(d.Sleep)
	default:
		resp.Data = req.Data
	}
	h.rec.Response(id, "DataTransfer", resp, nil)
	return resp, nil
}

var requests16 = map[string]func() ocpp.Request{
	"RemoteStartTransaction": func() ocpp.Request { return &core.RemoteStartTransactionRequest{} },
	"RemoteStopTransaction":  func() ocpp.Request { return &core.RemoteStopTransactionRequest{} },
	"Reset":                  func() ocpp.Request { return &core.ResetRequest{} },
	"ChangeConfiguration":    func() ocpp.Request { return &core.ChangeConfigurationRequest{} },
	"GetConfiguration":       func() ocpp.Request { return &core.GetConfigurationRequest{} },
	"ChangeAvailability":     func() ocpp.Request { return &core.ChangeAvailabilityRequest{} },
	"UnlockConnector":        func() ocpp.Request { return &core.UnlockConnectorRequest{} },
	"ClearCache":             func() ocpp.Request { return &core.ClearCacheRequest{} },
	"DataTransfer":           func() ocpp.Request { return &core.DataTransferRequest{} },
	"TriggerMessage":         func() ocpp.Request { return &remotetrigger.TriggerMessageRequest{} },
	"ReserveNow":             func() ocpp.Request { return &reservation.ReserveNowRequest{} },
	"CancelReservation":      func() ocpp.Request { return &reservation.CancelReservationRequest{} },
	"SetChargingProfile":     func() ocpp.Request { return &smartcharging.SetChargingProfileRequest{} },
	"ClearChargingProfile":   func() ocpp.Request { return &smartcharging.ClearChargingProfileRequest{} },
	"GetCompositeSchedule":   func() ocpp.Request { return &smartcharging.GetCompositeScheduleRequest{} },
	"GetLocalListVersion":    func() ocpp.Request { return &localauth.GetLocalListVersionRequest{} },
	"SendLocalList":          func() ocpp.Request { return &localauth.SendLocalListRequest{} },
	"UpdateFirmware":         func() ocpp.Request { return &firmware.UpdateFirmwareRequest{} },
	"GetDiagnostics":         func() ocpp.Request { return &firmware.GetDiagnosticsRequest{} },
}
