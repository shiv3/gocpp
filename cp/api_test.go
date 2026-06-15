package cp

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
)

type cfgReq struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
type cfgResp struct {
	Status string `json:"status"`
}

var changeCfg = ocppj.Message[cfgReq, cfgResp]{Action: "ChangeConfiguration", Direction: ocppj.SentByCSMS}

func TestCP_On_DirectionEnforced(t *testing.T) {
	c := NewClient("CP_1", "ws://x")
	// CP handling a SentByCSMS message is valid.
	require.NoError(t, On(c, changeCfg, func(ctx context.Context, req cfgReq) (cfgResp, error) {
		return cfgResp{Status: "Accepted"}, nil
	}))
	// CP handling a SentByCP message is invalid.
	bad := ocppj.Message[cfgReq, cfgResp]{Action: "Boot", Direction: ocppj.SentByCP}
	require.ErrorIs(t, On(c, bad, func(ctx context.Context, req cfgReq) (cfgResp, error) {
		return cfgResp{}, nil
	}), ocppj.ErrInvalidDirection)
}
