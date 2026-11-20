package ocppj

import (
	"time"

	"github.com/oklog/ulid/v2"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMsgID_Unique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := NewMsgID()
			mu.Lock()
			defer mu.Unlock()
			_, dup := seen[id]
			require.False(t, dup, "duplicate id: %s", id)
			seen[id] = struct{}{}
		}()
	}
	wg.Wait()
	require.Len(t, seen, n)
}

func TestNewMsgID_FormatAndOrder(t *testing.T) {
	a := NewMsgID()
	time.Sleep(3 * time.Millisecond)
	b := NewMsgID()
	for _, id := range []string{a, b} {
		require.Len(t, id, 26)
		require.LessOrEqual(t, len(id), MaxMessageIDLength)
		_, err := ulid.ParseStrict(id)
		require.NoError(t, err)
	}
	require.Less(t, a, b, "ids from different milliseconds sort by creation time")
}
