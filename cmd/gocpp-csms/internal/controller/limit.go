package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/dispatch"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/station"
	"github.com/shiv3/gocpp/core/ocppj"
)

// LimitRequest is the body of POST /chargepoints/{id}/limit.
type LimitRequest struct {
	// ConnectorID is the connector (1.6) or EVSE (2.x); 0 = the whole
	// charge point.
	ConnectorID int `json:"connectorId"`
	// Amps or Watts is the limit: exactly one is required. 0 pauses charging.
	Amps  *float64 `json:"amps,omitempty"`
	Watts *float64 `json:"watts,omitempty"`
	// Phases is the number of phases to charge on (numberPhases; also the
	// factor of the A <-> W conversion, default 3).
	Phases *int `json:"phases,omitempty"`
	// Purpose is "default" (TxDefaultProfile), "tx" (TxProfile of the running
	// transaction) or "max" (ChargePointMaxProfile / ChargingStationMaxProfile).
	Purpose    string `json:"purpose,omitempty"`
	StackLevel *int   `json:"stackLevel,omitempty"`
	ProfileID  *int   `json:"profileId,omitempty"`
	// Relative makes a Relative profile; the default is an Absolute one
	// starting a minute ago.
	Relative bool `json:"relative,omitempty"`
}

// Plan is a limit turned into a profile.
type Plan struct {
	Action  string
	Payload map[string]any
	Limit   station.Limit
}

// BuildLimit builds the SetChargingProfile of req for a charge point of version
// v with capabilities caps. txnID is the running transaction of the connector
// ("" = none; needed for purpose tx). It returns an error for an invalid
// request.
func BuildLimit(v ocppj.Version, req LimitRequest, caps station.Capabilities, txnID string, opts Options, now time.Time) (Plan, error) {
	if (req.Amps == nil) == (req.Watts == nil) {
		return Plan{}, fmt.Errorf("exactly one of amps and watts is required")
	}
	if req.Purpose == "" {
		req.Purpose = "default"
	}
	if req.ConnectorID < 0 {
		return Plan{}, fmt.Errorf("connectorId must not be negative")
	}
	phases := 3
	if req.Phases != nil {
		if *req.Phases != 1 && *req.Phases != 3 {
			return Plan{}, fmt.Errorf("phases must be 1 or 3")
		}
		phases = *req.Phases
	}
	volts := float64(opts.NominalVoltage)
	if volts <= 0 {
		volts = 230
	}
	// The value in the charge point's unit.
	unit := caps.RateUnit
	if unit != "W" {
		unit = "A"
	}
	var value float64
	switch {
	case req.Amps != nil && unit == "A":
		value = *req.Amps
	case req.Amps != nil:
		value = *req.Amps * volts * float64(phases)
	case unit == "W":
		value = *req.Watts
	default:
		value = *req.Watts / (volts * float64(phases))
	}
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return Plan{}, fmt.Errorf("limit must not be negative")
	}
	value = math.Floor(value*10) / 10 // 0.1 A / 0.1 W resolution

	var purpose string
	switch req.Purpose {
	case "default":
		purpose = "TxDefaultProfile"
	case "tx":
		purpose = "TxProfile"
		if txnID == "" {
			return Plan{}, &noTransactionError{req.ConnectorID}
		}
		if req.ConnectorID == 0 {
			return Plan{}, fmt.Errorf("purpose tx needs a connectorId > 0")
		}
	case "max":
		if v == ocppj.V16 {
			purpose = "ChargePointMaxProfile"
		} else {
			purpose = "ChargingStationMaxProfile"
		}
	default:
		return Plan{}, fmt.Errorf(`purpose must be "default", "tx" or "max"`)
	}
	stack := 0
	if caps.MaxStackLevel > 0 {
		stack = caps.MaxStackLevel
	}
	if req.StackLevel != nil {
		stack = *req.StackLevel
	}
	if stack < 0 {
		return Plan{}, fmt.Errorf("stackLevel must not be negative")
	}
	id := opts.ProfileID
	if req.ProfileID != nil {
		id = *req.ProfileID
	}
	kind := "Absolute"
	if req.Relative {
		kind = "Relative"
	}
	period := map[string]any{"startPeriod": 0, "limit": value}
	if req.Phases != nil {
		period["numberPhases"] = phases
	}
	schedule := map[string]any{
		"chargingRateUnit":       unit,
		"chargingSchedulePeriod": []any{period},
	}
	if !req.Relative {
		schedule["startSchedule"] = now.Add(-time.Minute).UTC().Format(time.RFC3339)
	}

	profile := map[string]any{
		"chargingProfileId":      id,
		"stackLevel":             stack,
		"chargingProfilePurpose": purpose,
		"chargingProfileKind":    kind,
	}
	plan := Plan{Limit: station.Limit{
		ConnectorID: req.ConnectorID, Purpose: req.Purpose, Unit: unit, Value: value,
		StackLevel: stack, ProfileID: id,
	}}
	if req.Phases != nil {
		plan.Limit.Phases = phases
	}
	if purpose == "TxProfile" {
		plan.Limit.TransactionID = txnID
	}
	if v == ocppj.V16 {
		if purpose == "TxProfile" {
			n, err := strconv.ParseInt(txnID, 10, 64)
			if err != nil {
				return Plan{}, fmt.Errorf("transaction id %q is not an integer", txnID)
			}
			profile["transactionId"] = n
		}
		profile["chargingSchedule"] = schedule
		plan.Action = "SetChargingProfile"
		plan.Payload = map[string]any{"connectorId": req.ConnectorID, "csChargingProfiles": profile}
		return plan, nil
	}
	if purpose == "TxProfile" {
		profile["transactionId"] = txnID
	}
	profile["id"] = id
	delete(profile, "chargingProfileId")
	schedule["id"] = id
	profile["chargingSchedule"] = []any{schedule}
	evse := req.ConnectorID
	if purpose == "ChargingStationMaxProfile" {
		evse = 0
		plan.Limit.ConnectorID = 0
	}
	plan.Action = "SetChargingProfile"
	plan.Payload = map[string]any{"evseId": evse, "chargingProfile": profile}
	return plan, nil
}

type noTransactionError struct{ connector int }

func (e *noTransactionError) Error() string {
	return fmt.Sprintf("connector %d has no running transaction (purpose tx)", e.connector)
}

func (e *Ext) connectorTxn(cp string, connector int) string {
	st, ok := e.reg.Snapshot(cp)
	if !ok {
		return ""
	}
	for _, c := range st.Connectors {
		if c.ID == connector {
			return c.TransactionID
		}
	}
	return ""
}

func (e *Ext) postLimit(w http.ResponseWriter, r *http.Request) {
	cp := r.PathValue("id")
	var req LimitRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, fmt.Sprintf("decode limit: %v", err))
		return
	}
	v, ok := e.version(cp)
	if !ok {
		httpapi.WriteError(w, http.StatusNotFound, fmt.Sprintf("charge point %q is not connected", cp))
		return
	}
	var caps station.Capabilities
	if st, ok := e.reg.Snapshot(cp); ok {
		caps = st.Capabilities
	} else {
		caps = station.Capabilities{RateUnit: "A", MaxStackLevel: -1}
	}
	plan, err := BuildLimit(v, req, caps, e.connectorTxn(cp, req.ConnectorID), e.Opts, e.now())
	if err != nil {
		status := http.StatusBadRequest
		var noTxn *noTransactionError
		if errors.As(err, &noTxn) {
			status = http.StatusConflict
		}
		httpapi.WriteError(w, status, err.Error())
		return
	}
	resp, err := e.send(r.Context(), cp, plan.Action, plan.Payload)
	if err != nil {
		dispatch.WriteSendError(w, err)
		return
	}
	var ans struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(resp, &ans)
	if ans.Status != "Accepted" {
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Response{OK: false, Error: "charge point answered " + ans.Status, Result: resp})
		return
	}
	e.reg.SetLimit(cp, plan.Limit)
	httpapi.WriteResult(w, map[string]any{"limit": plan.Limit, "request": plan.Payload, "response": resp})
}

// deleteLimit serves DELETE /chargepoints/{id}/limit?connectorId=&purpose=
// &stackLevel=: ClearChargingProfile by criteria (without any query
// parameter: the profile id the helper uses).
func (e *Ext) deleteLimit(w http.ResponseWriter, r *http.Request) {
	cp := r.PathValue("id")
	v, ok := e.version(cp)
	if !ok {
		httpapi.WriteError(w, http.StatusNotFound, fmt.Sprintf("charge point %q is not connected", cp))
		return
	}
	q := r.URL.Query()
	connector := -1
	if s := q.Get("connectorId"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			httpapi.WriteError(w, http.StatusBadRequest, "connectorId must be a non-negative integer")
			return
		}
		connector = n
	}
	purposeName := q.Get("purpose")
	purpose := ""
	switch purposeName {
	case "":
	case "default":
		purpose = "TxDefaultProfile"
	case "tx":
		purpose = "TxProfile"
	case "max":
		purpose = "ChargePointMaxProfile"
		if v != ocppj.V16 {
			purpose = "ChargingStationMaxProfile"
		}
	default:
		httpapi.WriteError(w, http.StatusBadRequest, `purpose must be "default", "tx" or "max"`)
		return
	}
	stack := -1
	if s := q.Get("stackLevel"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			httpapi.WriteError(w, http.StatusBadRequest, "stackLevel must be a non-negative integer")
			return
		}
		stack = n
	}
	payload := map[string]any{}
	byID := connector < 0 && purpose == "" && stack < 0
	if v == ocppj.V16 {
		switch {
		case byID:
			payload["id"] = e.Opts.ProfileID
		default:
			if connector >= 0 {
				payload["connectorId"] = connector
			}
			if purpose != "" {
				payload["chargingProfilePurpose"] = purpose
			}
			if stack >= 0 {
				payload["stackLevel"] = stack
			}
		}
	} else {
		crit := map[string]any{}
		if byID {
			payload["chargingProfileId"] = e.Opts.ProfileID
		} else {
			if connector >= 0 {
				crit["evseId"] = connector
			}
			if purpose != "" {
				crit["chargingProfilePurpose"] = purpose
			}
			if stack >= 0 {
				crit["stackLevel"] = stack
			}
			payload["chargingProfileCriteria"] = crit
		}
	}
	resp, err := e.send(r.Context(), cp, "ClearChargingProfile", payload)
	if err != nil {
		dispatch.WriteSendError(w, err)
		return
	}
	var ans struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(resp, &ans)
	if ans.Status != "Accepted" && ans.Status != "Unknown" {
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Response{OK: false, Error: "charge point answered " + ans.Status, Result: resp})
		return
	}
	e.reg.ClearLimits(cp, connector, purposeName)
	httpapi.WriteResult(w, resp)
}

// getSchedule serves GET /chargepoints/{id}/schedule?connectorId=&duration=
// &unit=: GetCompositeSchedule.
func (e *Ext) getSchedule(w http.ResponseWriter, r *http.Request) {
	cp := r.PathValue("id")
	v, ok := e.version(cp)
	if !ok {
		httpapi.WriteError(w, http.StatusNotFound, fmt.Sprintf("charge point %q is not connected", cp))
		return
	}
	q := r.URL.Query()
	intParam := func(name string, def int) (int, bool) {
		s := q.Get(name)
		if s == "" {
			return def, true
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			httpapi.WriteError(w, http.StatusBadRequest, name+" must be a non-negative integer")
			return 0, false
		}
		return n, true
	}
	connector, ok1 := intParam("connectorId", 0)
	duration, ok2 := intParam("duration", 60)
	if !ok1 || !ok2 {
		return
	}
	payload := map[string]any{"duration": duration}
	if v == ocppj.V16 {
		payload["connectorId"] = connector
	} else {
		payload["evseId"] = connector
	}
	if u := q.Get("unit"); u != "" {
		if u != "A" && u != "W" {
			httpapi.WriteError(w, http.StatusBadRequest, `unit must be "A" or "W"`)
			return
		}
		payload["chargingRateUnit"] = u
	}
	resp, err := e.send(r.Context(), cp, "GetCompositeSchedule", payload)
	if err != nil {
		dispatch.WriteSendError(w, err)
		return
	}
	httpapi.WriteResult(w, resp)
}
