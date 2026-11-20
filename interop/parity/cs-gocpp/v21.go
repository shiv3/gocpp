package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/interop/parity/common"
	v21h "github.com/shiv3/gocpp/v21/handlers"
	v21msg "github.com/shiv3/gocpp/v21/messages"
)

// handler21 implements the canned CP->CSMS answers for OCPP 2.1. Messages
// that are not listed keep the library default (NotSupported CallError).
type handler21 struct {
	v21h.UnimplementedCSMSHandler
	rec *common.Recorder
}

func info21(token string) v21msg.IdTokenInfoType {
	return v21msg.IdTokenInfoType{Status: common.AuthStatus(token)}
}

func (h *handler21) OnBootNotification(_ context.Context, c *csms.Conn, req v21msg.BootNotificationRequest) (v21msg.BootNotificationResponse, error) {
	h.rec.Request(c.ID(), "BootNotification", req)
	resp := v21msg.BootNotificationResponse{Status: "Accepted", Interval: common.BootInterval, CurrentTime: common.FixedTimeT}
	h.rec.Response(c.ID(), "BootNotification", resp, nil)
	return resp, nil
}

func (h *handler21) OnHeartbeat(_ context.Context, c *csms.Conn, req v21msg.HeartbeatRequest) (v21msg.HeartbeatResponse, error) {
	h.rec.Request(c.ID(), "Heartbeat", req)
	resp := v21msg.HeartbeatResponse{CurrentTime: common.FixedTimeT}
	h.rec.Response(c.ID(), "Heartbeat", resp, nil)
	return resp, nil
}

func (h *handler21) OnAuthorize(_ context.Context, c *csms.Conn, req v21msg.AuthorizeRequest) (v21msg.AuthorizeResponse, error) {
	h.rec.Request(c.ID(), "Authorize", req)
	resp := v21msg.AuthorizeResponse{IDTokenInfo: info21(req.IDToken.IDToken)}
	h.rec.Response(c.ID(), "Authorize", resp, nil)
	return resp, nil
}

func (h *handler21) OnTransactionEvent(_ context.Context, c *csms.Conn, req v21msg.TransactionEventRequest) (v21msg.TransactionEventResponse, error) {
	h.rec.Request(c.ID(), "TransactionEvent", req)
	var resp v21msg.TransactionEventResponse
	if req.IDToken != nil {
		i := info21(req.IDToken.IDToken)
		resp.IDTokenInfo = &i
	}
	h.rec.Response(c.ID(), "TransactionEvent", resp, nil)
	return resp, nil
}

func (h *handler21) OnStatusNotification(_ context.Context, c *csms.Conn, req v21msg.StatusNotificationRequest) (v21msg.StatusNotificationResponse, error) {
	h.rec.Request(c.ID(), "StatusNotification", req)
	resp := v21msg.StatusNotificationResponse{}
	h.rec.Response(c.ID(), "StatusNotification", resp, nil)
	return resp, nil
}

func (h *handler21) OnMeterValues(_ context.Context, c *csms.Conn, req v21msg.MeterValuesRequest) (v21msg.MeterValuesResponse, error) {
	h.rec.Request(c.ID(), "MeterValues", req)
	resp := v21msg.MeterValuesResponse{}
	h.rec.Response(c.ID(), "MeterValues", resp, nil)
	return resp, nil
}

func (h *handler21) OnNotifyReport(_ context.Context, c *csms.Conn, req v21msg.NotifyReportRequest) (v21msg.NotifyReportResponse, error) {
	h.rec.Request(c.ID(), "NotifyReport", req)
	resp := v21msg.NotifyReportResponse{}
	h.rec.Response(c.ID(), "NotifyReport", resp, nil)
	return resp, nil
}

func (h *handler21) OnDataTransfer(_ context.Context, c *csms.Conn, req v21msg.DataTransferRequest) (v21msg.DataTransferResponse, error) {
	h.rec.Request(c.ID(), "DataTransfer", req)
	var data any
	if len(req.Data) > 0 {
		_ = json.Unmarshal(req.Data, &data)
	}
	d := common.DecideDataTransfer(req.VendorID, data)
	switch d.Kind {
	case common.DTPanic:
		h.rec.PanicPending(c.ID(), "DataTransfer")
		panic(common.PanicMessage)
	case common.DTError:
		err := errors.New(common.ErrorMessage)
		h.rec.Response(c.ID(), "DataTransfer", nil, err)
		return v21msg.DataTransferResponse{}, err
	}
	resp := v21msg.DataTransferResponse{Status: "Accepted"}
	switch d.Kind {
	case common.DTReject:
		resp.Status = "Rejected"
	case common.DTSleep:
		time.Sleep(d.Sleep)
	default:
		resp.Data = req.Data
	}
	h.rec.Response(c.ID(), "DataTransfer", resp, nil)
	return resp, nil
}

func register21(srv *csms.Server, rec *common.Recorder) error {
	return v21h.RegisterCSMS(srv, &handler21{rec: rec})
}

// OnNotifyPeriodicEventStream handles the OCPP 2.1 SEND NotifyPeriodicEventStream
// (no response): it only records the request.
func (h *handler21) OnNotifyPeriodicEventStream(_ context.Context, c *csms.Conn, req v21msg.NotifyPeriodicEventStream) error {
	h.rec.Request(c.ID(), "NotifyPeriodicEventStream", req)
	return nil
}
