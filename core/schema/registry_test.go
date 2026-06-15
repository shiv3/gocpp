package schema_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	v16schemas "github.com/shiv3/gocpp/v16/schemas"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RegisterAndValidate(t *testing.T) {
	r := schema.NewRegistry()
	require.NoError(t, r.Register("1.6", "BootNotification", "request", v16schemas.FS, "BootNotification.json"))

	v, ok := r.Lookup("1.6", "BootNotification", "request")
	require.True(t, ok)
	require.NoError(t, v.Validate([]byte(`{"chargePointVendor":"X","chargePointModel":"Y"}`)))

	_, ok = r.Lookup("1.6", "Unknown", "request")
	require.False(t, ok)
}
