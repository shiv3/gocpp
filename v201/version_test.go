package v201_test

import (
	"testing"

	"github.com/shiv3/gocpp/v201"
	"github.com/stretchr/testify/require"
)

func TestVersionConstants(t *testing.T) {
	require.Equal(t, "2.0.1", v201.Version)
	require.Equal(t, "ocpp2.0.1", v201.SubProtocol)
}
