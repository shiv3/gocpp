package dispatcher

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPendingStore_AddResolveRemove(t *testing.T) {
	ps := newPendingStore()
	ch := make(chan rawResult, 1)
	ps.add("id1", &pendingCall{msgID: "id1", action: "Authorize", respCh: ch})

	// resolve delivers the result exactly once
	ok := ps.resolve("id1", rawResult{payload: []byte(`{"a":1}`)})
	require.True(t, ok)
	require.Equal(t, `{"a":1}`, string((<-ch).payload))

	// second resolve is a no-op (already removed)
	ok = ps.resolve("id1", rawResult{payload: []byte(`{}`)})
	require.False(t, ok)
}

func TestPendingStore_FailAll(t *testing.T) {
	ps := newPendingStore()
	ch1 := make(chan rawResult, 1)
	ch2 := make(chan rawResult, 1)
	ps.add("a", &pendingCall{msgID: "a", respCh: ch1})
	ps.add("b", &pendingCall{msgID: "b", respCh: ch2})

	ps.failAll(errExample)

	require.ErrorIs(t, (<-ch1).err, errExample)
	require.ErrorIs(t, (<-ch2).err, errExample)
	require.Equal(t, 0, ps.len())
}

var errExample = errors.New("example")
