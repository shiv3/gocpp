package ocppj

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse_Leniency(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		typ      MessageType
		code     string
		desc     string
		wantData string // "" means ErrData must be nil
	}{
		{"call result with extra element", `[3,"a",{"x":1},"extra"]`, CallResult, "", "", ""},
		{"call error without details", `[4,"a","InternalError","boom"]`, MessageTypeCallError, "InternalError", "boom", ""},
		{"call error non-string description", `[4,"a","InternalError",42,{}]`, MessageTypeCallError, "InternalError", "", `{}`},
		{"call result error without details", `[5,"a","FormatViolation","bad"]`, MessageTypeCallResultError, "FormatViolation", "bad", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse([]byte(tc.raw))
			require.NoError(t, err)
			require.Equal(t, tc.typ, f.Type)
			require.Equal(t, "a", f.MsgID)
			require.Equal(t, tc.code, f.ErrCode)
			require.Equal(t, tc.desc, f.ErrDesc)
			if tc.wantData == "" {
				require.Nil(t, f.ErrData)
			} else {
				require.JSONEq(t, tc.wantData, string(f.ErrData))
			}
		})
	}
}

func TestParse_TooShortIsProtocolError(t *testing.T) {
	for _, raw := range []string{`[3,"a"]`, `[4,"a","Code"]`, `[5,"a","Code"]`} {
		_, err := Parse([]byte(raw))
		var pe *ProtocolError
		require.ErrorAs(t, err, &pe, raw)
	}
}

func TestParse_ProtocolErrorCarriesMessageTypeAndID(t *testing.T) {
	cases := []struct {
		name, raw string
		mt        int
		id        string
	}{
		{"call wrong arity", `[2,"id1","Heartbeat"]`, 2, "id1"},
		{"call extra element", `[2,"id1","Heartbeat",{},1]`, 2, "id1"},
		{"call action not a string", `[2,"id1",5,{}]`, 2, "id1"},
		{"send wrong arity", `[6,"id2","X"]`, 6, "id2"},
		{"message id not a string", `[2,5,"Heartbeat",{}]`, 0, ""},
		{"too short", `[2]`, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.raw))
			var pe *ProtocolError
			require.ErrorAs(t, err, &pe)
			require.Equal(t, tc.mt, pe.MessageType)
			require.Equal(t, tc.id, pe.MsgID)
		})
	}
}
