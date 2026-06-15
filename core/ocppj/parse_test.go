package ocppj

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Frame
	}{
		{
			name: "call",
			raw:  `[2,"abc","BootNotification",{"chargePointVendor":"X"}]`,
			want: Frame{Type: Call, MsgID: "abc", Action: "BootNotification", Payload: []byte(`{"chargePointVendor":"X"}`)},
		},
		{
			name: "call result",
			raw:  `[3,"abc",{"status":"Accepted"}]`,
			want: Frame{Type: CallResult, MsgID: "abc", Payload: []byte(`{"status":"Accepted"}`)},
		},
		{
			name: "call error",
			raw:  `[4,"abc","NotImplemented","not impl",{}]`,
			want: Frame{Type: MessageTypeCallError, MsgID: "abc", ErrCode: "NotImplemented", ErrDesc: "not impl", ErrData: []byte(`{}`)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse([]byte(tc.raw))
			require.NoError(t, err)
			require.Equal(t, tc.want.Type, got.Type)
			require.Equal(t, tc.want.MsgID, got.MsgID)
			require.Equal(t, tc.want.Action, got.Action)
			if tc.want.Payload != nil {
				require.JSONEq(t, string(tc.want.Payload), string(got.Payload))
			}
			require.Equal(t, tc.want.ErrCode, got.ErrCode)
			require.Equal(t, tc.want.ErrDesc, got.ErrDesc)
			if tc.want.ErrData != nil {
				require.JSONEq(t, string(tc.want.ErrData), string(got.ErrData))
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"not json", `not json`},
		{"not array", `{"a":1}`},
		{"empty array", `[]`},
		{"bad type", `[9,"x"]`},
		{"call too short", `[2,"x"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.raw))
			var pe *ProtocolError
			require.ErrorAs(t, err, &pe)
		})
	}
}
