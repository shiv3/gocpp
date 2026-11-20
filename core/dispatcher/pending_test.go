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

// The first pending call lives inline and the map appears only for a second
// concurrent one; every entry resolves exactly once wherever it is stored.
func TestPendingStore_InlineEntryAndLazyMap(t *testing.T) {
	ps := newPendingStore()
	chs := map[string]chan rawResult{}
	add := func(id string) {
		chs[id] = make(chan rawResult, 1)
		ps.add(id, &pendingCall{msgID: id, respCh: chs[id]})
	}
	add("a")
	require.Nil(t, ps.more, "a single pending call must not allocate the map")
	require.Equal(t, 1, ps.len())
	add("b")
	add("c")
	require.Equal(t, 3, ps.len())
	require.NotNil(t, ps.more)

	require.True(t, ps.resolve("b", rawResult{payload: []byte("b")}))
	require.Equal(t, "b", string((<-chs["b"]).payload))
	require.True(t, ps.resolve("a", rawResult{payload: []byte("a")})) // the inline slot
	require.Equal(t, "a", string((<-chs["a"]).payload))
	require.False(t, ps.resolve("a", rawResult{}))
	require.Equal(t, 1, ps.len())

	add("d") // the inline slot is free again
	ps.remove("c")
	ps.remove("zzz") // unknown id is a no-op
	require.Equal(t, 1, ps.len())
	ps.failAll(errExample)
	require.ErrorIs(t, (<-chs["d"]).err, errExample)
	require.Equal(t, 0, ps.len())
}
