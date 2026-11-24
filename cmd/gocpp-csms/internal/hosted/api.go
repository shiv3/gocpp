package hosted

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
)

// InstanceInfo is the answer of GET /instance.
type InstanceInfo struct {
	Mode       string `json:"mode"`
	InstanceID string `json:"instanceId,omitempty"`
	// ConnectionLimit is nil without a limit.
	ConnectionLimit   *int   `json:"connectionLimit"`
	ActiveConnections int    `json:"activeConnections"`
	Registrations     int    `json:"registrations"`
	WSBaseURL         string `json:"wsBaseUrl"`
}

// RegistrationInfo is a registration as served (never the hash).
type RegistrationInfo struct {
	CPID              string    `json:"cpId"`
	DisplayName       string    `json:"displayName"`
	WSURL             string    `json:"wsUrl"`
	CreatedAt         time.Time `json:"createdAt"`
	PasswordChangedAt time.Time `json:"passwordChangedAt"`
	Connected         bool      `json:"connected"`
}

// Credentials is the answer of a registration or password rotation: the
// password is shown this once.
type Credentials struct {
	CPID        string `json:"cpId"`
	DisplayName string `json:"displayName"`
	WSURL       string `json:"wsUrl"`
	Password    string `json:"password"`
}

const maxBody = 64 << 10

func (in *Instance) routes(h *server.Hooks) {
	h.Handle("GET /instance", http.HandlerFunc(in.getInstance))
	h.Handle("PUT /instance/mode", http.HandlerFunc(in.putMode))
	h.Handle("POST /instance/clear", http.HandlerFunc(in.clear))
	h.Handle("GET /registrations", http.HandlerFunc(in.list))
	h.Handle("POST /registrations", http.HandlerFunc(in.register))
	h.Handle("DELETE /registrations/{cpId}", http.HandlerFunc(in.delete))
	h.Handle("POST /registrations/{cpId}/password", http.HandlerFunc(in.rotate))
}

// Info returns the instance information.
func (in *Instance) Info() InstanceInfo {
	info := InstanceInfo{
		Mode:              in.reg.Mode(),
		InstanceID:        in.instanceID,
		ActiveConnections: in.ActiveConnections(),
		Registrations:     len(in.reg.List()),
		WSBaseURL:         in.baseURL(),
	}
	if l := int(in.limit.Load()); l >= 0 {
		info.ConnectionLimit = &l
	}
	return info
}

func (in *Instance) getInstance(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteResult(w, in.Info())
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (in *Instance) putMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Mode != ModeOpen && body.Mode != ModePrivate {
		httpapi.WriteError(w, http.StatusBadRequest, `mode must be "open" or "private"`)
		return
	}
	if err := in.reg.SetMode(body.Mode); err != nil {
		in.fail(w, err)
		return
	}
	in.logger.Info("instance mode changed", "mode", body.Mode)
	httpapi.WriteResult(w, in.Info())
}

func (in *Instance) clear(w http.ResponseWriter, _ *http.Request) {
	n, err := in.reg.Clear()
	if err != nil {
		in.fail(w, err)
		return
	}
	in.logger.Info("registrations cleared", "deleted", n)
	httpapi.WriteResult(w, map[string]int{"deleted": n})
}

func (in *Instance) list(w http.ResponseWriter, _ *http.Request) {
	regs := in.reg.List()
	out := make([]RegistrationInfo, 0, len(regs))
	for _, reg := range regs {
		out = append(out, RegistrationInfo{
			CPID: reg.CPID, DisplayName: reg.DisplayName, WSURL: in.WSURL(reg.CPID),
			CreatedAt: reg.CreatedAt, PasswordChangedAt: reg.PasswordChangedAt,
			Connected: in.isConnected(reg.CPID),
		})
	}
	httpapi.WriteResult(w, out)
}

func (in *Instance) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CPID        string `json:"cpId"`
		DisplayName string `json:"displayName"`
	}
	if !decode(w, r, &body) {
		return
	}
	reg, password, err := in.reg.Register(body.CPID, body.DisplayName)
	if err != nil {
		in.fail(w, err)
		return
	}
	in.logger.Info("charge point registered", "cp_id", reg.CPID)
	httpapi.WriteJSON(w, http.StatusCreated, httpapi.Response{OK: true, Result: Credentials{
		CPID: reg.CPID, DisplayName: reg.DisplayName, WSURL: in.WSURL(reg.CPID), Password: password,
	}})
}

func (in *Instance) delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("cpId")
	if err := in.reg.Delete(id); err != nil {
		in.fail(w, err)
		return
	}
	in.logger.Info("charge point registration deleted", "cp_id", id)
	httpapi.WriteResult(w, map[string]string{"cpId": id})
}

func (in *Instance) rotate(w http.ResponseWriter, r *http.Request) {
	reg, password, err := in.reg.Rotate(r.PathValue("cpId"))
	if err != nil {
		in.fail(w, err)
		return
	}
	in.logger.Info("charge point password rotated", "cp_id", reg.CPID)
	httpapi.WriteResult(w, Credentials{CPID: reg.CPID, DisplayName: reg.DisplayName, WSURL: in.WSURL(reg.CPID), Password: password})
}

func (in *Instance) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidID), errors.Is(err, ErrDisplayName):
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrOpenMode), errors.Is(err, ErrExists):
		httpapi.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotFound):
		httpapi.WriteError(w, http.StatusNotFound, err.Error())
	default:
		in.logger.Error("registry", "error", err)
		httpapi.WriteError(w, http.StatusInternalServerError, "registry update failed")
	}
}
