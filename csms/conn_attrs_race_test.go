package csms

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

// SetAttrs derives the new session from the old one; a concurrent epoch update
// by the reconcile loop must not be overwritten with the old epoch (review NIT:
// the epoch could go back).
func TestConnSetAttrs_DoesNotRollBackEpoch(t *testing.T) {
	f := transport.NewFakeWS("ocpp1.6")
	dc := dispatcher.NewConn("CP_1", f, dispatcher.DefaultConfig(), dispatcher.NewHandlerRegistry())
	dc.SetAttachment(&sessionRef{s: storage.Session{CPID: "CP_1", Epoch: 1}})
	dc.Start(context.Background())
	defer func() { _ = dc.Close(nil) }()
	c := &Conn{inner: dc}

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			_ = c.SetAttrs(context.Background(), map[string]string{"k": fmt.Sprint(i % 10)})
		}
	}()
	go func() {
		defer wg.Done()
		for e := uint64(2); !stop.Load(); e++ {
			c.setHolderSession(storage.Session{CPID: "CP_1", Epoch: e})
		}
	}()
	var last uint64
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s, _ := c.Session()
		require.GreaterOrEqual(t, s.Epoch, last, "the session epoch went back")
		last = s.Epoch
	}
	stop.Store(true)
	wg.Wait()
}
