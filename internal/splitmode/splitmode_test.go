package splitmode_test

import (
	"testing"

	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/shiv3/gocpp/internal/splitmode"
	"github.com/stretchr/testify/require"
)

func TestNewServerModes(t *testing.T) {
	t.Setenv("GOCPP_TEST_SPLIT", "")
	splitmode.NewServer().Close()

	t.Setenv("GOCPP_TEST_SPLIT", "1")
	splitmode.NewServer().Close()

	t.Setenv("GOCPP_TEST_SPLIT", "nope")
	require.PanicsWithValue(t, `splitmode: GOCPP_TEST_SPLIT="nope" names no registered transport`, func() { splitmode.NewServer() })

	// A registered transport is built once per server.
	built := 0
	splitmode.Register("counting", func() csms.SplitLink { built++; return inproc.New() })
	t.Setenv("GOCPP_TEST_SPLIT", "counting")
	splitmode.NewServer().Close()
	splitmode.NewServer().Close()
	require.Equal(t, 2, built)
}
