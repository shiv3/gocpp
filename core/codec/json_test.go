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
