package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoot_HasRunCommand(t *testing.T) {
	root := Root()
	require.Equal(t, "gocpp-sim", root.Name())
	_, _, err := root.Find([]string{"run"})
	require.NoError(t, err)
}
