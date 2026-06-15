package v201_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v201"
	"github.com/stretchr/testify/require"
)

func TestRegistry_AllMessagesRegistered(t *testing.T) {
	r := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(r))

	_, ok := r.Lookup("2.0.1", "BootNotification", "request")
	require.True(t, ok)
	_, ok = r.Lookup("2.0.1", "TransactionEvent", "request")
	require.True(t, ok)
}
