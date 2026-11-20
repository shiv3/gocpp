package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/interop/parity/common"
	v201h "github.com/shiv3/gocpp/v201/handlers"
	v201msg "github.com/shiv3/gocpp/v201/messages"
)

// handler201 implements the canned CP->CSMS answers for OCPP 2.0.1. Messages
// that are not listed keep the library default (NotSupported CallError).
type handler201 struct {
	v201h.UnimplementedCSMSHandler
	rec *common.Recorder
}

func info201(token string) v201msg.IdTokenInfoType {
	return v201msg.IdTokenInfoType{Status: common.AuthStatus(token)}
}

func (h *handler201) OnBootNotification(_ context.Context, c *csms.Conn, req v201msg.BootNotificationRequest) (v201msg.BootNotificationResponse, error) {
	h.rec.Request(c.ID(), "BootNotification", req)
	resp := v201msg.BootNotificationResponse{Status: "Accepted", Interval: common.BootInterval, CurrentTime: common.FixedTimeT}
	h.rec.Response(c.ID(), "BootNotification", resp, nil)
	return resp, nil
}

func (h *handler201) OnHeartbeat(_ context.Context, c *csms.Conn, req v201msg.HeartbeatRequest) (v201msg.HeartbeatResponse, error) {
	h.rec.Request(c.ID(), "Heartbeat", req)
	resp := v201msg.HeartbeatResponse{CurrentTime: common.FixedTimeT}
	h.rec.Response(c.ID(), "Heartbeat", resp, nil)
	return resp, nil
}

func (h *handler201) OnAuthorize(_ context.Context, c *csms.Conn, req v201msg.AuthorizeRequest) (v201msg.AuthorizeResponse, error) {
	h.rec.Request(c.ID(), "Authorize", req)
	resp := v201msg.AuthorizeResponse{IDTokenInfo: info201(req.IDToken.IDToken)}
	h.rec.Response(c.ID(), "Authorize", resp, nil)
	return resp, nil
}

func (h *handler201) OnTransactionEvent(_ context.Context, c *csms.Conn, req v201msg.TransactionEventRequest) (v201msg.TransactionEventResponse, error) {
	h.rec.Request(c.ID(), "TransactionEvent", req)
	var resp v201msg.TransactionEventResponse
	if req.IDToken != nil {
		i := info201(req.IDToken.IDToken)
		resp.IDTokenInfo = &i
	}
	h.rec.Response(c.ID(), "TransactionEvent", resp, nil)
	return resp, nil
}

func (h *handler201) OnStatusNotification(_ context.Context, c *csms.Conn, req v201msg.StatusNotificationRequest) (v201msg.StatusNotificationResponse, error) {
	h.rec.Request(c.ID(), "StatusNotification", req)
	resp := v201msg.StatusNotificationResponse{}
	h.rec.Response(c.ID(), "StatusNotification", resp, nil)
	return resp, nil
}

func (h *handler201) OnMeterValues(_ context.Context, c *csms.Conn, req v201msg.MeterValuesRequest) (v201msg.MeterValuesResponse, error) {
	h.rec.Request(c.ID(), "MeterValues", req)
	resp := v201msg.MeterValuesResponse{}
	h.rec.Response(c.ID(), "MeterValues", resp, nil)
	return resp, nil
}

func (h *handler201) OnNotifyReport(_ context.Context, c *csms.Conn, req v201msg.NotifyReportRequest) (v201msg.NotifyReportResponse, error) {
	h.rec.Request(c.ID(), "NotifyReport", req)
	resp := v201msg.NotifyReportResponse{}
	h.rec.Response(c.ID(), "NotifyReport", resp, nil)
	return resp, nil
}

func (h *handler201) OnDataTransfer(_ context.Context, c *csms.Conn, req v201msg.DataTransferRequest) (v201msg.DataTransferResponse, error) {
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
		return v201msg.DataTransferResponse{}, err
	}
	resp := v201msg.DataTransferResponse{Status: "Accepted"}
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

func register201(srv *csms.Server, rec *common.Recorder) error {
	return v201h.RegisterCSMS(srv, &handler201{rec: rec})
}
