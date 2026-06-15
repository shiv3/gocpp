package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/stretchr/testify/require"
)

type fakeConn struct{ id string }

func (f fakeConn) ID() string        { return f.id }
func (f fakeConn) Close(error) error { return nil }

func TestMemoryRegistry(t *testing.T) {
	r := memory.NewConnectionRegistry()
	ctx := context.Background()
	require.NoError(t, r.PutLocal(ctx, "CP_1", fakeConn{"CP_1"}))
	got, ok := r.GetLocal("CP_1")
	require.True(t, ok)
	require.Equal(t, "CP_1", got.ID())
	require.NoError(t, r.DeleteLocal(ctx, "CP_1"))
	_, ok = r.GetLocal("CP_1")
	require.False(t, ok)
	require.NoError(t, r.PutGlobal(ctx, "CP_1", "inst-1"))
	_, ok, err := r.LookupGlobal(ctx, "CP_1")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMemoryTransactionStore(t *testing.T) {
	s := memory.NewTransactionStore()
	ctx := context.Background()
	require.NoError(t, s.Begin(ctx, storage.Transaction{ID: "t1", CPID: "CP_1", StartedAt: time.Now(), Status: storage.TransactionActive}))
	active, err := s.ListActive(ctx, "CP_1")
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.NoError(t, s.End(ctx, "t1", storage.TransactionEnd{EndedAt: time.Now(), MeterStop: 50, Status: storage.TransactionCompleted}))
	active, err = s.ListActive(ctx, "CP_1")
	require.NoError(t, err)
	require.Empty(t, active)
}

func TestMemoryConfigStore(t *testing.T) {
	s := memory.NewConfigStore()
	ctx := context.Background()
	require.NoError(t, s.Put(ctx, "CP_1", "HeartbeatInterval", "60"))
	v, ok, err := s.Get(ctx, "CP_1", "HeartbeatInterval")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "60", v)
}

func TestMemoryRouter_SingleInstance(t *testing.T) {
	r := memory.NewRouter()
	_, err := r.CallRemote(context.Background(), "CP_1", "Reset", []byte(`{}`))
	require.ErrorIs(t, err, storage.ErrRouterNotImplemented)
}

func TestInProcessRouter_ForwardsBetweenInstances(t *testing.T) {
	hub := memory.NewInProcessHub()
	instA := hub.Router("A")
	instB := hub.Router("B")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go instB.ServeRemote(ctx, func(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
		require.Equal(t, "CP_1", cpID)
		return []byte(`{"status":"Accepted"}`), nil
	})
	hub.Bind("CP_1", "B")

	require.Eventually(t, func() bool {
		resp, err := instA.CallRemote(ctx, "CP_1", "Reset", []byte(`{}`))
		return err == nil && string(resp) == `{"status":"Accepted"}`
	}, 2*time.Second, 10*time.Millisecond)
}
