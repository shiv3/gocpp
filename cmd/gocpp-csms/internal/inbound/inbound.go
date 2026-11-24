// Package inbound answers the CALLs charge points send to gocpp-csms in serve
// mode: every charge-point-initiated action of OCPP 1.6, 2.0.1 and 2.1 gets a
// schema-valid default answer that accepts it ("accept-all"), with real
// transaction semantics on top (see package txn).
//
// Extension points, for features layered on serve mode:
//
//   - Override replaces the answer of one action of one version; the
//     override may call Default to start from the built-in answer.
//   - Observe registers a function told about every handled CALL and SEND
//     with the answer that was sent (state models, message logs).
//
// Register both before the server starts serving.
package inbound

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/actions"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/recorder"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/txn"
	"github.com/shiv3/gocpp/core/codec"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

// Call is one inbound CALL (or 2.1 SEND) from a charge point.
type Call struct {
	// Conn is the charge point connection; nil in unit tests.
	Conn    *csms.Conn
	CPID    string
	Version ocppj.Version
	Action  string
	Payload json.RawMessage
	// Send is true for an unconfirmed 2.1 SEND (the answer is discarded).
	Send bool
}

// Responder produces the answer to a CALL. A returned *ocppj.CallError is
// sent as that CALLERROR.
type Responder func(ctx context.Context, call Call) (json.RawMessage, error)

// Observer is told about a handled CALL after its answer was produced (resp
// is nil for a SEND or when err is set).
type Observer func(ctx context.Context, call Call, resp json.RawMessage, err error)

// Options configure a Handler.
type Options struct {
	// Txns is the transaction store (required).
	Txns *txn.Store
	// Recorder, if set, gets one line per CALL before it is answered.
	Recorder *recorder.Recorder
	// BootInterval is the interval of BootNotification answers (seconds).
	BootInterval int
	Logger       *slog.Logger
	// Now is the clock (time.Now if nil).
	Now func() time.Time
}

// Handler answers inbound CALLs.
type Handler struct {
	opts Options

	mu        sync.RWMutex
	overrides map[string]Responder // version|action
	observers []Observer
}

// New returns a Handler.
func New(opts Options) *Handler {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.BootInterval < 1 {
		opts.BootInterval = 300
	}
	return &Handler{opts: opts, overrides: map[string]Responder{}}
}

// Override makes r answer action on connections of version v.
func (h *Handler) Override(v ocppj.Version, action string, r Responder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.overrides[string(v)+"|"+action] = r
}

// Observe registers an observer.
func (h *Handler) Observe(o Observer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.observers = append(h.observers, o)
}

// Register installs the handler on srv: a catch-all for every CALL and a
// handler for the 2.1 SEND NotifyPeriodicEventStream.
func (h *Handler) Register(srv *csms.Server) error {
	csms.OnAny(srv, func(ctx context.Context, c *csms.Conn, action string, payload []byte) ([]byte, error) {
		return h.Handle(ctx, Call{Conn: c, CPID: c.ID(), Version: c.Version(), Action: action, Payload: payload})
	})
	return csms.OnSendVersion(srv, ocppj.V21, v21profiles.NotifyPeriodicEventStream, func(ctx context.Context, c *csms.Conn, req v21msg.NotifyPeriodicEventStream) error {
		payload, err := codec.Marshal(req)
		if err != nil {
			return err
		}
		_, err = h.Handle(ctx, Call{Conn: c, CPID: c.ID(), Version: c.Version(), Action: v21profiles.NotifyPeriodicEventStream.Action, Payload: payload, Send: true})
		return err
	})
}

// Handle records, answers and reports one inbound CALL.
func (h *Handler) Handle(ctx context.Context, call Call) (json.RawMessage, error) {
	known := actions.SentByCP(call.Version)
	if call.Send {
		if !known.HasSend(call.Action) {
			return nil, ocppj.NewCallError(ocppj.ErrorCodeNotSupported, fmt.Sprintf("%s is not a %s SEND of a charge point", call.Action, call.Version), nil)
		}
	} else if !known.Has(call.Action) {
		return nil, ocppj.NewCallError(ocppj.ErrorCodeNotSupported, fmt.Sprintf("%s is not a %s action sent by a charge point", call.Action, call.Version), nil)
	}
	if rec := h.opts.Recorder; rec != nil {
		if err := rec.Record(call.CPID, call.Action, call.Payload); err != nil {
			h.opts.Logger.ErrorContext(ctx, "record frame", "error", err)
		}
	}

	h.mu.RLock()
	r := h.overrides[string(call.Version)+"|"+call.Action]
	observers := h.observers
	h.mu.RUnlock()

	var (
		resp json.RawMessage
		err  error
	)
	if r != nil {
		resp, err = r(ctx, call)
	} else {
		resp, err = h.Default(ctx, call)
	}
	if call.Send {
		resp = nil
	}
	for _, o := range observers {
		o(ctx, call, resp, err)
	}
	return resp, err
}

// Default is the built-in accept-all answer, including the transaction store
// updates.
func (h *Handler) Default(ctx context.Context, call Call) (json.RawMessage, error) {
	if call.Send {
		return nil, nil
	}
	switch call.Action {
	case "BootNotification":
		return marshal(map[string]any{"currentTime": h.now(), "interval": h.opts.BootInterval, "status": "Accepted"})
	case "Heartbeat":
		return marshal(map[string]any{"currentTime": h.now()})
	case "DataTransfer", "SignCertificate", "GetCertificateStatus", "NotifyEVChargingNeeds",
		"NotifyEVChargingSchedule", "RequestBatterySwap", "PullDynamicScheduleUpdate":
		return json.RawMessage(`{"status":"Accepted"}`), nil
	case "Get15118EVCertificate":
		return json.RawMessage(`{"status":"Accepted","exiResponse":""}`), nil
	case "Authorize":
		if call.Version == ocppj.V16 {
			return json.RawMessage(`{"idTagInfo":{"status":"Accepted"}}`), nil
		}
		return json.RawMessage(`{"idTokenInfo":{"status":"Accepted"}}`), nil
	case "StartTransaction":
		return h.startTransaction16(call)
	case "StopTransaction":
		return h.stopTransaction16(call)
	case "MeterValues":
		if call.Version == ocppj.V16 {
			h.meterValues16(call)
		}
		return json.RawMessage(`{}`), nil
	case "TransactionEvent":
		return h.transactionEvent(call)
	case "VatNumberValidation":
		return h.vatNumberValidation(call)
	case "GetCertificateChainStatus":
		return h.certificateChainStatus(call)
	default:
		return json.RawMessage(`{}`), nil
	}
}

func (h *Handler) now() string {
	return h.opts.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return b, err
}

func decode(call Call, v any) error {
	if err := json.Unmarshal(call.Payload, v); err != nil {
		return ocppj.NewCallError(ocppj.ErrorCodeFormationViolation, fmt.Sprintf("decode %s: %v", call.Action, err), nil)
	}
	return nil
}

type startTransaction16 struct {
	ConnectorID int       `json:"connectorId"`
	IDTag       string    `json:"idTag"`
	MeterStart  float64   `json:"meterStart"`
	Timestamp   time.Time `json:"timestamp"`
}

func (h *Handler) startTransaction16(call Call) (json.RawMessage, error) {
	var req startTransaction16
	if err := decode(call, &req); err != nil {
		return nil, err
	}
	t := h.opts.Txns.Start16(call.CPID, req.ConnectorID, req.IDTag, req.MeterStart, req.Timestamp)
	id, _ := strconv.ParseInt(t.ID, 10, 64)
	return marshal(map[string]any{"idTagInfo": map[string]string{"status": "Accepted"}, "transactionId": id})
}

type stopTransaction16 struct {
	TransactionID int64     `json:"transactionId"`
	IDTag         string    `json:"idTag"`
	MeterStop     float64   `json:"meterStop"`
	Timestamp     time.Time `json:"timestamp"`
	Reason        string    `json:"reason"`
}

func (h *Handler) stopTransaction16(call Call) (json.RawMessage, error) {
	var req stopTransaction16
	if err := decode(call, &req); err != nil {
		return nil, err
	}
	if _, ok := h.opts.Txns.Stop16(call.CPID, req.TransactionID, req.MeterStop, req.Timestamp, req.Reason); !ok {
		h.opts.Logger.Warn("StopTransaction for an unknown transaction", "cp_id", call.CPID, "transaction_id", req.TransactionID)
	}
	if req.IDTag != "" {
		return json.RawMessage(`{"idTagInfo":{"status":"Accepted"}}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (h *Handler) meterValues16(call Call) {
	var req struct {
		TransactionID *int64 `json:"transactionId"`
	}
	if json.Unmarshal(call.Payload, &req) == nil && req.TransactionID != nil {
		h.opts.Txns.Touch16(call.CPID, *req.TransactionID)
	}
}

type transactionEvent struct {
	EventType       string    `json:"eventType"`
	Timestamp       time.Time `json:"timestamp"`
	SeqNo           *int64    `json:"seqNo"`
	TransactionInfo struct {
		TransactionID string `json:"transactionId"`
		ChargingState string `json:"chargingState"`
		StoppedReason string `json:"stoppedReason"`
	} `json:"transactionInfo"`
	IDToken *struct {
		IDToken string `json:"idToken"`
	} `json:"idToken"`
	EVSE *struct {
		ID          int `json:"id"`
		ConnectorID int `json:"connectorId"`
	} `json:"evse"`
	MeterValue []struct {
		SampledValue []struct {
			Value         flexNumber `json:"value"`
			Measurand     string     `json:"measurand"`
			UnitOfMeasure *struct {
				Unit       string `json:"unit"`
				Multiplier int    `json:"multiplier"`
			} `json:"unitOfMeasure"`
		} `json:"sampledValue"`
	} `json:"meterValue"`
}

func (h *Handler) transactionEvent(call Call) (json.RawMessage, error) {
	var req transactionEvent
	if err := decode(call, &req); err != nil {
		return nil, err
	}
	ev := txn.Event{
		Version:       string(call.Version),
		EventType:     req.EventType,
		TransactionID: req.TransactionInfo.TransactionID,
		Timestamp:     req.Timestamp,
		SeqNo:         req.SeqNo,
		ChargingState: req.TransactionInfo.ChargingState,
		StoppedReason: req.TransactionInfo.StoppedReason,
	}
	if req.EVSE != nil {
		ev.EVSEID, ev.ConnectorID = req.EVSE.ID, req.EVSE.ConnectorID
	}
	if req.IDToken != nil {
		ev.IDToken = req.IDToken.IDToken
	}
	for _, mv := range req.MeterValue {
		for _, sv := range mv.SampledValue {
			if sv.Measurand != "" && sv.Measurand != "Energy.Active.Import.Register" {
				continue
			}
			wh := float64(sv.Value)
			if u := sv.UnitOfMeasure; u != nil {
				if u.Unit == "kWh" {
					wh *= 1000
				}
				wh *= math.Pow10(u.Multiplier)
			}
			ev.Meter = &wh
		}
	}
	h.opts.Txns.Apply(call.CPID, ev)
	if req.IDToken != nil {
		return json.RawMessage(`{"idTokenInfo":{"status":"Accepted"}}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (h *Handler) vatNumberValidation(call Call) (json.RawMessage, error) {
	var req struct {
		VATNumber string `json:"vatNumber"`
		EVSEID    *int   `json:"evseId,omitempty"`
	}
	if err := decode(call, &req); err != nil {
		return nil, err
	}
	resp := map[string]any{"vatNumber": req.VATNumber, "status": "Accepted"}
	if req.EVSEID != nil {
		resp["evseId"] = *req.EVSEID
	}
	return marshal(resp)
}

// certificateChainStatus answers every requested certificate as Good, valid
// for another day.
func (h *Handler) certificateChainStatus(call Call) (json.RawMessage, error) {
	var req struct {
		Requests []struct {
			Source              string          `json:"source"`
			CertificateHashData json.RawMessage `json:"certificateHashData"`
		} `json:"certificateStatusRequests"`
	}
	if err := decode(call, &req); err != nil {
		return nil, err
	}
	next := h.opts.Now().Add(24 * time.Hour).UTC().Format("2006-01-02T15:04:05.000Z07:00")
	statuses := make([]map[string]any, 0, len(req.Requests))
	for _, r := range req.Requests {
		statuses = append(statuses, map[string]any{
			"source":              r.Source,
			"status":              "Good",
			"nextUpdate":          next,
			"certificateHashData": r.CertificateHashData,
		})
	}
	return marshal(map[string]any{"certificateStatus": statuses})
}

// flexNumber decodes a JSON number or a numeric string (lenient chargers);
// anything else decodes as 0 rather than failing the whole message.
type flexNumber float64

func (n *flexNumber) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*n = flexNumber(f)
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			*n = flexNumber(f)
		}
	}
	return nil
}
