package transport

import (
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestCompressionModeCoder(t *testing.T) {
	require.Equal(t, websocket.CompressionDisabled, CompressionDisabled.Coder())
	require.Equal(t, websocket.CompressionContextTakeover, CompressionContextTakeover.Coder())
	require.Equal(t, websocket.CompressionNoContextTakeover, CompressionNoContextTakeover.Coder())
}
