package v21_test

import (
	"testing"

	"github.com/shiv3/gocpp/v21"
	"github.com/stretchr/testify/require"
)

func TestVersionConstants(t *testing.T) {
	require.Equal(t, "2.1", v21.Version)
	require.Equal(t, "ocpp2.1", v21.SubProtocol)
}
