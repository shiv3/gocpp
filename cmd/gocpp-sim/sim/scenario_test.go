package sim

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScenario(t *testing.T) {
	yaml := `
version: "1.6"
cpId: CP_SIM
csmsUrl: ws://localhost:8080/ocpp/
steps:
  - action: BootNotification
    payload: { chargePointVendor: Acme, chargePointModel: M1 }
  - action: Heartbeat
    payload: {}
    delayMs: 500
`
	s, err := ParseScenario([]byte(yaml))
	require.NoError(t, err)
	require.Equal(t, "1.6", s.Version)
	require.Equal(t, "CP_SIM", s.CPID)
	require.Len(t, s.Steps, 2)
	require.Equal(t, "BootNotification", s.Steps[0].Action)
	require.Equal(t, 500, s.Steps[1].DelayMs)
}

func TestParseScenario_MissingFields(t *testing.T) {
	_, err := ParseScenario([]byte(`version: "1.6"`))
	require.Error(t, err)
}
