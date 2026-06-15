package csms_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

type bootReq struct {
	ChargePointVendor string `json:"chargePointVendor"`
	ChargePointModel  string `json:"chargePointModel"`
}
type bootResp struct {
	Status   string `json:"status"`
	Interval int    `json:"interval"`
}

var bootMsg = ocppj.Message[bootReq, bootResp]{Action: "BootNotification", Direction: ocppj.SentByCP}

func TestOn_RegistersTypedHandler(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	called := make(chan bootReq, 1)
	err := csms.On(srv, bootMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		called <- req
		return bootResp{Status: "Accepted", Interval: 300}, nil
	})
	require.NoError(t, err)

	// Wrong-direction registration is rejected.
	badMsg := ocppj.Message[bootReq, bootResp]{Action: "X", Direction: ocppj.SentByCSMS}
	err = csms.On(srv, badMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		return bootResp{}, nil
	})
	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
	_ = called
	_ = time.Second
}
