package ocppj

import "testing"

func TestMessageType_String(t *testing.T) {
	cases := []struct {
		mt   MessageType
		want string
	}{
		{Call, "Call"},
		{CallResult, "CallResult"},
		{MessageTypeCallError, "CallError"},
		{MessageType(99), "Unknown(99)"},
	}
	for _, tc := range cases {
		if got := tc.mt.String(); got != tc.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", tc.mt, got, tc.want)
		}
	}
}
