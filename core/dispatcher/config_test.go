package dispatcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	require.Equal(t, 30*time.Second, c.CallTimeout)
	require.Equal(t, 10*time.Second, c.WriteTimeout)
	require.Equal(t, 64, c.OutboundQueueSize)
	require.Equal(t, int64(16), c.MaxConcurrentHandlers)
	require.NotNil(t, c.Metrics)
	require.Equal(t, SchemaModeOff, c.SchemaMode)
}

func TestSchemaModeLenientDistinct(t *testing.T) {
	require.NotEqual(t, SchemaModeStrict, SchemaModeLenient)
	require.NotEqual(t, SchemaModeOff, SchemaModeLenient)
	require.NotEqual(t, SchemaModeTolerant, SchemaModeLenient)
}
