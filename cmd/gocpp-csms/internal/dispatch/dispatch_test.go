package dispatch

import (
	"errors"
	"net/http"
	"testing"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/actions"
	"github.com/shiv3/gocpp/core/ocppj"
)

func TestParseCommand(t *testing.T) {
	for _, tc := range []struct {
		body, payload, err string
	}{
		{`{"cpId":"CP1","action":"Reset","payload":{"type":"Soft"}}`, `{"type":"Soft"}`, ""},
		{`{"cpId":"CP1","action":"Reset","type":"Soft"}`, `{"type":"Soft"}`, ""},
		{`{"cpId":"CP1","action":"ClearCache"}`, `{}`, ""},
		{`{"cpId":"CP1","action":"Reset","payload":{},"type":"Soft"}`, "", `decode command: "payload" cannot be combined with other request fields`},
		{`{"action":"Reset"}`, "", "cpId is required"},
		{`{"cpId":"CP1"}`, "", "action is required"},
		{`{"cpId":1,"action":"Reset"}`, "", "decode command: cpId: json: cannot unmarshal number into Go value of type string"},
	} {
		cmd, err := ParseCommand([]byte(tc.body))
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("%s: err %v, want %q", tc.body, err, tc.err)
			}
			continue
		}
		if err != nil || string(cmd.Payload) != tc.payload || cmd.CPID != "CP1" {
			t.Errorf("%s: %+v %v", tc.body, cmd, err)
		}
	}
}

func TestValidate(t *testing.T) {
	reg, err := actions.Schemas()
	if err != nil {
		t.Fatal(err)
	}
	d := New(nil, reg, 0)
	if err := d.Validate(ocppj.V16, "Reset", []byte(`{"type":"Soft"}`)); err != nil {
		t.Fatal(err)
	}
	var e *Error
	if err := d.Validate(ocppj.V16, "Reset", []byte(`{"type":"Sideways"}`)); !errors.As(err, &e) || e.Status != http.StatusBadRequest {
		t.Fatalf("invalid payload: %v", err)
	}
	if err := d.Validate(ocppj.V201, "RemoteStartTransaction", []byte(`{}`)); err == nil || err.Error() != `unsupported 2.0.1 action "RemoteStartTransaction"` {
		t.Fatalf("1.6 action on 2.0.1: %v", err)
	}
	if err := d.Validate(ocppj.V16, "BootNotification", []byte(`{}`)); err == nil {
		t.Fatal("a charge point action was accepted as a command")
	}
	for _, v := range actions.Versions {
		if n := len(actions.SentByCSMS(v).Calls); n < 20 {
			t.Fatalf("%s: %d CSMS actions", v, n)
		}
	}
}
