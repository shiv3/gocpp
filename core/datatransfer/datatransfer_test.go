package datatransfer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalUnmarshal(t *testing.T) {
	type payload struct {
		Foo string `json:"foo"`
		N   int    `json:"n"`
	}
	in := payload{Foo: "bar", N: 7}

	s, err := Marshal(in)
	require.NoError(t, err)
	require.NotNil(t, s)
	require.JSONEq(t, `{"foo":"bar","n":7}`, *s)

	out, err := Unmarshal[payload](s)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestUnmarshal_NilOrEmpty(t *testing.T) {
	type payload struct {
		Foo string `json:"foo"`
	}
	out, err := Unmarshal[payload](nil)
	require.NoError(t, err)
	require.Equal(t, payload{}, out)

	empty := ""
	out, err = Unmarshal[payload](&empty)
	require.NoError(t, err)
	require.Equal(t, payload{}, out)
}
