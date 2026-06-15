package diff

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDiffMessageSets(t *testing.T) {
	oldSet := map[string][]string{
		"BootNotification": {"reason", "chargingStation"},
		"Authorize":        {"idToken"},
	}
	newSet := map[string][]string{
		"BootNotification": {"reason", "chargingStation", "customData"},
		"Authorize":        {"idToken"},
		"SetDERControl":    {"isDefault", "controlType"},
	}
	d := Compute(oldSet, newSet)
	require.Equal(t, []string{"SetDERControl"}, d.AddedMessages)
	require.Empty(t, d.RemovedMessages)
	require.Contains(t, d.ChangedMessages["BootNotification"].AddedFields, "customData")
}

func TestDiff_Markdown(t *testing.T) {
	d := Compute(
		map[string][]string{"Authorize": {"idToken"}},
		map[string][]string{"Authorize": {"idToken"}, "BatterySwap": {"requestId"}},
	)
	md := d.Markdown("2.0.1", "2.1")
	require.Contains(t, md, "## OCPP 2.0.1 → 2.1")
	require.Contains(t, md, "### Added messages")
	require.Contains(t, md, "- BatterySwap")
}
