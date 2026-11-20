package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type extraDemo struct {
	VendorID string                     `json:"vendorId"`
	Extra    map[string]json.RawMessage `json:"-"`
}

func TestExtraRoundTrip(t *testing.T) {
	type plain extraDemo
	var d extraDemo
	extra, err := UnmarshalWithExtra([]byte(`{"vendorId":"x","foo":1,"bar":{"a":true}}`), (*plain)(&d), "vendorId")
	require.NoError(t, err)
	d.Extra = extra
	require.Equal(t, "x", d.VendorID)
	require.JSONEq(t, `1`, string(d.Extra["foo"]))

	out, err := MarshalWithExtra(plain(d), d.Extra)
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"x","foo":1,"bar":{"a":true}}`, string(out))
}

func TestExtraDefinedFieldWins(t *testing.T) {
	type plain extraDemo
	out, err := MarshalWithExtra(plain{VendorID: "real"}, map[string]json.RawMessage{"vendorId": json.RawMessage(`"fake"`)})
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"real"}`, string(out))
}

func TestUnmarshalWithExtraNone(t *testing.T) {
	type plain extraDemo
	var d extraDemo
	extra, err := UnmarshalWithExtra([]byte(`{"vendorId":"x"}`), (*plain)(&d), "vendorId")
	require.NoError(t, err)
	require.Nil(t, extra)
	_, err = UnmarshalWithExtra([]byte(`"str"`), (*plain)(&d), "vendorId")
	require.Error(t, err)
}
