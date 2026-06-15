package v21_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v21"
	"github.com/stretchr/testify/require"
)

func TestRegistry_AllMessagesRegistered(t *testing.T) {
	r := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(r))

	_, ok := r.Lookup("2.1", "BootNotification", "request")
	require.True(t, ok)
	_, ok = r.Lookup("2.1", "SetDERControl", "request")
	require.True(t, ok)
}
