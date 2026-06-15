package schema_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	v16schemas "github.com/shiv3/gocpp/v16/schemas"
	"github.com/stretchr/testify/require"
)

func TestValidator_BootNotification(t *testing.T) {
	v, err := schema.New(v16schemas.FS, "BootNotification.json")
	require.NoError(t, err)

	// valid
	require.NoError(t, v.Validate([]byte(`{"chargePointVendor":"X","chargePointModel":"Y"}`)))

	// missing required field
	err = v.Validate([]byte(`{"chargePointVendor":"X"}`))
	require.Error(t, err)

	// maxLength violation
	err = v.Validate([]byte(`{"chargePointVendor":"012345678901234567890123","chargePointModel":"Y"}`))
	require.Error(t, err)
}
