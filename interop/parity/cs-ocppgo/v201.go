package main

import (
	"errors"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocpp2 "github.com/lorenzodonini/ocpp-go/ocpp2.0.1"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/authorization"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/availability"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/data"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/diagnostics"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/display"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/iso15118"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/localauth"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/meter"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/provisioning"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/remotecontrol"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/reservation"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/security"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/tariffcost"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/transactions"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/parity/common"
)

// setup201 wires an ocpp-go OCPP 2.0.1 CSMS with the profiles that carry
// CP->CSMS messages. Profiles without a handler answer NotImplemented.
func setup201(rec *common.Recorder, wsServer *ws.Server, dispatcher ocppj.ServerDispatcher, hk hooks) (csAPI, map[string]func() ocpp.Request) {
	endpoint := ocppj.NewServer(wsServer, dispatcher, nil,
		authorization.Profile, availability.Profile, data.Profile, meter.Profile,
		provisioning.Profile, transactions.Profile, security.Profile, diagnostics.Profile, firmware.Profile,
		display.Profile, iso15118.Profile, localauth.Profile, remotecontrol.Profile, reservation.Profile,
		smartcharging.Profile, tariffcost.Profile)
	cs := ocpp2.NewCSMS(endpoint, wsServer)
	h := &handler201{rec: rec}
	cs.SetAuthorizationHandler(h)
	cs.SetAvailabilityHandler(h)
	cs.SetDataHandler(h)
	cs.SetMeterHandler(h)
	cs.SetProvisioningHandler(h)
	cs.SetTransactionsHandler(h)
	cs.SetNewChargingStationValidationHandler(hk.validate)
	cs.SetNewChargingStationHandler(func(cp ocpp2.ChargingStationConnection) { hk.connect(cp.ID()) })
	cs.SetChargingStationDisconnectedHandler(func(cp ocpp2.ChargingStationConnection) { hk.disconnect(cp.ID()) })
	return cs, requests201
}

type handler201 struct {
	rec *common.Recorder
	tx  common.TxCounter
}

func info201(token string) types.IdTokenInfo {
	return types.IdTokenInfo{Status: types.AuthorizationStatus(common.AuthStatus(token))}
}

func (h *handler201) OnBootNotification(id string, req *provisioning.BootNotificationRequest) (*provisioning.BootNotificationResponse, error) {
	h.rec.Request(id, "BootNotification", req)
	resp := provisioning.NewBootNotificationResponse(types.NewDateTime(common.FixedTimeT), common.BootInterval, provisioning.RegistrationStatusAccepted)
	h.rec.Response(id, "BootNotification", resp, nil)
	return resp, nil
}

func (h *handler201) OnNotifyReport(id string, req *provisioning.NotifyReportRequest) (*provisioning.NotifyReportResponse, error) {
	h.rec.Request(id, "NotifyReport", req)
	resp := provisioning.NewNotifyReportResponse()
	h.rec.Response(id, "NotifyReport", resp, nil)
	return resp, nil
}

func (h *handler201) OnHeartbeat(id string, req *availability.HeartbeatRequest) (*availability.HeartbeatResponse, error) {
	h.rec.Request(id, "Heartbeat", req)
	resp := availability.NewHeartbeatResponse(*types.NewDateTime(common.FixedTimeT))
	h.rec.Response(id, "Heartbeat", resp, nil)
	return resp, nil
}

func (h *handler201) OnStatusNotification(id string, req *availability.StatusNotificationRequest) (*availability.StatusNotificationResponse, error) {
	h.rec.Request(id, "StatusNotification", req)
	resp := availability.NewStatusNotificationResponse()
	h.rec.Response(id, "StatusNotification", resp, nil)
	return resp, nil
}

func (h *handler201) OnAuthorize(id string, req *authorization.AuthorizeRequest) (*authorization.AuthorizeResponse, error) {
	h.rec.Request(id, "Authorize", req)
	resp := authorization.NewAuthorizationResponse(info201(req.IdToken.IdToken))
	h.rec.Response(id, "Authorize", resp, nil)
	return resp, nil
}

func (h *handler201) OnTransactionEvent(id string, req *transactions.TransactionEventRequest) (*transactions.TransactionEventResponse, error) {
	h.rec.Request(id, "TransactionEvent", req)
	resp := transactions.NewTransactionEventResponse()
	if req.IDToken != nil {
		i := info201(req.IDToken.IdToken)
		resp.IDTokenInfo = &i
	}
	h.rec.Response(id, "TransactionEvent", resp, nil)
	return resp, nil
}

func (h *handler201) OnMeterValues(id string, req *meter.MeterValuesRequest) (*meter.MeterValuesResponse, error) {
	h.rec.Request(id, "MeterValues", req)
	resp := meter.NewMeterValuesResponse()
	h.rec.Response(id, "MeterValues", resp, nil)
	return resp, nil
}

func (h *handler201) OnDataTransfer(id string, req *data.DataTransferRequest) (*data.DataTransferResponse, error) {
	h.rec.Request(id, "DataTransfer", req)
	d := common.DecideDataTransfer(req.VendorID, req.Data)
	switch d.Kind {
	case common.DTPanic:
		h.rec.PanicPending(id, "DataTransfer")
		panic(common.PanicMessage)
	case common.DTError:
		err := errors.New(common.ErrorMessage)
		h.rec.Response(id, "DataTransfer", nil, err)
		return nil, err
	}
	resp := data.NewDataTransferResponse(data.DataTransferStatusAccepted)
	switch d.Kind {
	case common.DTReject:
		resp.Status = data.DataTransferStatusRejected
	case common.DTSleep:
		time.Sleep(d.Sleep)
	default:
		resp.Data = req.Data
	}
	h.rec.Response(id, "DataTransfer", resp, nil)
	return resp, nil
}
