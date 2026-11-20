package types_test

import (
	"encoding/json"
	"testing"

	"github.com/shiv3/gocpp/core/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type holder struct {
	Req types.Decimal  `json:"req"`
	Opt *types.Decimal `json:"opt,omitempty"`
}

func TestDecimalMarshalIsUnquotedWithGlobalDefault(t *testing.T) {
	require.False(t, decimal.MarshalJSONWithoutQuotes, "test must run with the global at its default")
	v := types.NewFromFloat(32.5)
	b, err := json.Marshal(holder{Req: v, Opt: &v})
	require.NoError(t, err)
	require.JSONEq(t, `{"req":32.5,"opt":32.5}`, string(b))
	require.Contains(t, string(b), `"req":32.5`)
}

func TestDecimalNilPointerOmitted(t *testing.T) {
	b, err := json.Marshal(holder{Req: types.NewFromInt(7)})
	require.NoError(t, err)
	require.Equal(t, `{"req":7}`, string(b))
}

func TestDecimalUnmarshalNumberAndString(t *testing.T) {
	for _, in := range []string{`{"req":32.5,"opt":32.5}`, `{"req":"32.5","opt":"32.5"}`} {
		var h holder
		require.NoError(t, json.Unmarshal([]byte(in), &h), in)
		require.True(t, h.Req.Equal(decimal.RequireFromString("32.5")), in)
		require.NotNil(t, h.Opt, in)
		require.Equal(t, "32.5", h.Opt.String())
	}
	var h holder
	require.NoError(t, json.Unmarshal([]byte(`{"req":1,"opt":null}`), &h))
	require.Nil(t, h.Opt)
	require.Error(t, json.Unmarshal([]byte(`{"req":"abc"}`), &h))
}
