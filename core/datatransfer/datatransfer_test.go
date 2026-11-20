package datatransfer

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type payload struct {
	Foo string `json:"foo"`
	N   int    `json:"n"`
}

func TestMarshalUnmarshal(t *testing.T) {
	in := payload{Foo: "bar", N: 7}

	raw, err := Marshal(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"foo":"bar","n":7}`, string(raw))

	out, err := Unmarshal[payload](raw)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestUnmarshal_NilOrEmpty(t *testing.T) {
	out, err := Unmarshal[payload](nil)
	require.NoError(t, err)
	require.Equal(t, payload{}, out)

	out, err = Unmarshal[payload](json.RawMessage{})
	require.NoError(t, err)
	require.Equal(t, payload{}, out)
}

func TestUnmarshal_NonObjectTypes(t *testing.T) {
	s, err := Unmarshal[string](json.RawMessage(`"hello"`))
	require.NoError(t, err)
	require.Equal(t, "hello", s)

	_, err = Unmarshal[payload](json.RawMessage(`"not an object"`))
	require.Error(t, err)
}

func TestMarshalStringUnmarshalString(t *testing.T) {
	in := payload{Foo: "bar", N: 7}
	s, err := MarshalString(in)
	require.NoError(t, err)
	require.JSONEq(t, `{"foo":"bar","n":7}`, *s)

	out, err := UnmarshalString[payload](s)
	require.NoError(t, err)
	require.Equal(t, in, out)

	out, err = UnmarshalString[payload](nil)
	require.NoError(t, err)
	require.Equal(t, payload{}, out)
	empty := ""
	out, err = UnmarshalString[payload](&empty)
	require.NoError(t, err)
	require.Equal(t, payload{}, out)
}
