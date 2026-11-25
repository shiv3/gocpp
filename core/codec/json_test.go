package codec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshal(t *testing.T) {
	type x struct {
		A string `json:"a"`
	}
	b, err := Marshal(x{A: "hello"})
	require.NoError(t, err)
	require.JSONEq(t, `{"a":"hello"}`, string(b))

	var got x
	require.NoError(t, Unmarshal(b, &got))
	require.Equal(t, "hello", got.A)
}
func TestUnmarshalIntegralFloatForInt(t *testing.T) {
	var v struct {
		N int64   `json:"n"`
		F float64 `json:"f"`
		S string  `json:"s"`
	}
	if err := Unmarshal([]byte(`{"n":20000.0,"f":1.5,"s":"1.0 \"2.0\""}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.N != 20000 || v.F != 1.5 || v.S != `1.0 "2.0"` {
		t.Fatalf("got %+v", v)
	}
	if err := Unmarshal([]byte(`{"n":2e4}`), &v); err != nil || v.N != 20000 {
		t.Fatalf("exponent: %v %+v", err, v)
	}
	if err := Unmarshal([]byte(`{"n":1.5}`), &v); err == nil {
		t.Fatal("fractional value must still be rejected")
	}
	if err := Unmarshal([]byte(`{"n":"x"}`), &v); err == nil {
		t.Fatal("string for int must still be rejected")
	}
}
