package v16_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
	"github.com/stretchr/testify/require"
)

func TestRegistry_AllMessagesRegistered(t *testing.T) {
	r := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(r))

	v, ok := r.Lookup("1.6", "BootNotification", "request")
	require.True(t, ok)
	require.NoError(t, v.Validate([]byte(`{"chargePointVendor":"X","chargePointModel":"Y"}`)))

	_, ok = r.Lookup("1.6", "Authorize", "request")
	require.True(t, ok)
}
