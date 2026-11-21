package e2e

import (
	"context"
	"encoding/json"
	"github.com/shiv3/gocpp/internal/splitmode"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/types"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/calls"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func setChargingProfileReq() v16msg.SetChargingProfileRequest {
	return v16msg.SetChargingProfileRequest{
		ConnectorID: 1,
		CsChargingProfiles: v16msg.CsChargingProfiles{
			ChargingProfileID:      1,
			StackLevel:             0,
			ChargingProfilePurpose: v16msg.CsChargingProfilesChargingProfilePurposeTxDefaultProfile,
			ChargingProfileKind:    v16msg.CsChargingProfilesChargingProfileKindAbsolute,
			ChargingSchedule: v16msg.ChargingSchedule{
				ChargingRateUnit:       v16msg.ChargingScheduleChargingRateUnitA,
				MinChargingRate:        ptrDecimal(types.NewFromFloat(6.5)),
				ChargingSchedulePeriod: []v16msg.ChargingSchedulePeriod{{StartPeriod: 0, Limit: types.NewFromFloat(32.5)}},
			},
		},
	}
}

func ptrDecimal(d types.Decimal) *types.Decimal { return &d }

func TestSetChargingProfileLimitMarshalsAsJSONNumber(t *testing.T) {
	require.False(t, decimal.MarshalJSONWithoutQuotes)
	b, err := json.Marshal(setChargingProfileReq())
	require.NoError(t, err)
	require.Contains(t, string(b), `"limit":32.5`)
	require.Contains(t, string(b), `"minChargingRate":6.5`)

	for _, in := range []string{`"limit":32.5`, `"limit":"32.5"`} {
		var m map[string]any
		require.NoError(t, json.Unmarshal(b, &m))
		s := string(b)
		if in == `"limit":"32.5"` {
			s = `{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":"32.5"}]}}}`
		}
		var got v16msg.SetChargingProfileRequest
		require.NoError(t, json.Unmarshal([]byte(s), &got))
		require.Equal(t, "32.5", got.CsChargingProfiles.ChargingSchedule.ChargingSchedulePeriod[0].Limit.String())
	}
}

func TestE2E_StrictOutgoingSetChargingProfile(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))

	srv := splitmode.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithSchemaRegistry(reg),
		csms.WithStrictSchema(true),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	var gotLimit string
	require.NoError(t, cp.On(client, v16p.SetChargingProfile, func(_ context.Context, req v16msg.SetChargingProfileRequest) (v16msg.SetChargingProfileResponse, error) {
		gotLimit = req.CsChargingProfiles.ChargingSchedule.ChargingSchedulePeriod[0].Limit.String()
		return v16msg.SetChargingProfileResponse{Status: v16msg.SetChargingProfileResponseStatusAccepted}, nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	var conn *csms.Conn
	require.Eventually(t, func() bool {
		c, ok := srv.Get("CP_1")
		if ok {
			conn = c
		}
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	resp, err := calls.CSMSSetChargingProfile(ctx, conn, setChargingProfileReq())
	require.NoError(t, err)
	require.Equal(t, v16msg.SetChargingProfileResponseStatusAccepted, resp.Status)
	require.Equal(t, "32.5", gotLimit)
}
