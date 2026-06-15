package v16_test

import (
	"testing"

	"github.com/shiv3/gocpp/v16"
	"github.com/stretchr/testify/require"
)

func TestVersionConstants(t *testing.T) {
	require.Equal(t, "1.6", v16.Version)
	require.Equal(t, "ocpp1.6", v16.SubProtocol)
}
