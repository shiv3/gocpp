package ocppj

import (
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
