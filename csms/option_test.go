package csms

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOptions_Apply(t *testing.T) {
	cfg := defaultServerConfig()
	WithCallTimeout(5 * time.Second).apply(&cfg)
	WithSubProtocols("ocpp1.6", "ocpp2.0.1").apply(&cfg)
	WithLogger(slog.Default()).apply(&cfg)

	require.Equal(t, 5*time.Second, cfg.dispatcher.CallTimeout)
	require.Equal(t, []string{"ocpp1.6", "ocpp2.0.1"}, cfg.subProtocols)
	require.NotNil(t, cfg.dispatcher.Logger)
}
