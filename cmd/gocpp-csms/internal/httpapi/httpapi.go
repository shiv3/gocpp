// Package httpapi has the JSON envelope shared by the serve-mode control
// endpoints of gocpp-csms:
//
//	{"ok":true,"result":...}
//	{"ok":false,"error":"...","errorCode":"..."}
//
// It is the envelope of the legacy control API, plus errorCode (the OCPP
// error code when a charge point answered with a CALLERROR).
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Response is the envelope of every control endpoint.
type Response struct {
	OK        bool   `json:"ok"`
	Result    any    `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
}

// WriteJSON writes resp with status.
func WriteJSON(w http.ResponseWriter, status int, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// WriteResult writes {"ok":true,"result":result} with status 200.
func WriteResult(w http.ResponseWriter, result any) {
	WriteJSON(w, http.StatusOK, Response{OK: true, Result: result})
}

// WriteError writes {"ok":false,"error":msg} with status.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, Response{OK: false, Error: msg})
}

// AllowMethods answers 405 unless r uses one of methods; it reports
// whether the request may proceed.
func AllowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}
