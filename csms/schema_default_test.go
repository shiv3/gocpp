package csms_test

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func hbRig(t *testing.T, opts ...csms.Option) *coerceRig {
	t.Helper()
	rig := newCoerceRig(t, opts...)
	require.NoError(t, csms.On(rig.srv, profiles.StatusNotification, func(_ context.Context, _ *csms.Conn, _ messages.StatusNotificationRequest) (messages.StatusNotificationResponse, error) {
		return messages.StatusNotificationResponse{}, nil
	}))
	return rig
}

const missingRequired = `[2,"1","StatusNotification",{"connectorId":1}]`

// A registry without a schema option validates leniently: a message missing
// required properties is rejected, a benign enum case slip is repaired.
func TestSchemaDefault_RegistryAloneIsLenient(t *testing.T) {
	rig := hbRig(t)
	require.Contains(t, rig.call(t, missingRequired), `[4,"1","OccurenceConstraintViolation"`)
	require.Contains(t, rig.call(t, `[2,"2","StatusNotification",{"connectorId":1,"errorCode":"noerror","status":"available"}]`), `[3,"2"`)
}

func TestSchemaDefault_ExplicitOffStaysOff(t *testing.T) {
	rig := hbRig(t, csms.WithStrictSchema(false))
	require.Contains(t, rig.call(t, missingRequired), `[3,"1"`)
}

func TestSchemaDefault_ExplicitModesWin(t *testing.T) {
	rig := hbRig(t, csms.WithTolerantSchema())
	require.Contains(t, rig.call(t, missingRequired), `[3,"1"`, "tolerant logs and passes")
	rig = hbRig(t, csms.WithStrictSchema(true))
	require.Contains(t, rig.call(t, `[2,"2","StatusNotification",{"connectorId":1,"errorCode":"noerror","status":"available"}]`), `[4,"2"`, "strict rejects the case slip")
}
