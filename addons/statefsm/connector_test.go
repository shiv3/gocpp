package statefsm

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

func TestLegalChargeSessionTransitions(t *testing.T) {
	c := New("cp-1", 1)

	assertState(t, c, StateAvailable)
	assertNoError(t, c.PlugIn())
	assertState(t, c, StatePreparing)
	assertNoError(t, c.Authorize())
	assertState(t, c, StatePreparing)
	assertNoError(t, c.StartTransaction(TransactionBegin{ID: "tx-1"}))
	assertState(t, c, StateCharging)
	assertNoError(t, c.SuspendEV())
	assertState(t, c, StateSuspendedEV)
	assertNoError(t, c.Resume())
	assertState(t, c, StateCharging)
	assertNoError(t, c.SuspendEVSE())
	assertState(t, c, StateSuspendedEVSE)
	assertNoError(t, c.SuspendEV())
	assertState(t, c, StateSuspendedEV)
	assertNoError(t, c.StopTransaction(TransactionEnd{ID: "tx-1"}))
	assertState(t, c, StateFinishing)
	assertNoError(t, c.Unplug())
	assertState(t, c, StateAvailable)
}

func TestReservationAvailabilityAndFaultTransitions(t *testing.T) {
	c := New("cp-1", 1)

	assertNoError(t, c.Reserve())
	assertState(t, c, StateReserved)
	assertNoError(t, c.CancelReservation())
	assertState(t, c, StateAvailable)
	assertNoError(t, c.ChangeAvailability())
	assertState(t, c, StateUnavailable)
	assertNoError(t, c.ChangeAvailability())
	assertState(t, c, StateAvailable)
	assertNoError(t, c.Fault())
	assertState(t, c, StateFaulted)
	assertNoError(t, c.ClearFault())
	assertState(t, c, StateAvailable)
}

func TestAuthorizeBeforePlugIn(t *testing.T) {
	c := New("cp-1", 1)

	assertNoError(t, c.Authorize())
	assertState(t, c, StatePreparing)
	assertNoError(t, c.PlugIn())
	assertState(t, c, StatePreparing)
	assertNoError(t, c.StartTransaction(TransactionBegin{}))
	assertState(t, c, StateCharging)
}

func TestIllegalTransitionRejected(t *testing.T) {
	c := New("cp-1", 1)

	if err := c.StartTransaction(TransactionBegin{}); err == nil {
		t.Fatal("expected StartTransaction from Available to fail")
	}
	assertState(t, c, StateAvailable)

	if err := c.CancelReservation(); err == nil {
		t.Fatal("expected CancelReservation from Available to fail")
	}
	assertState(t, c, StateAvailable)
}

func TestMemoryStateStorePersistence(t *testing.T) {
	store := NewMemoryStateStore()
	c := New("cp-1", 1, WithStateStore(store))

	assertNoError(t, c.Reserve())

	state, ok, err := store.Load("cp-1", 1)
	assertNoError(t, err)
	if !ok {
		t.Fatal("expected saved state")
	}
	if state != StateReserved {
		t.Fatalf("saved state = %s, want %s", state, StateReserved)
	}

	reloaded := New("cp-1", 1, WithStateStore(store))
	assertState(t, reloaded, StateReserved)
	assertNoError(t, reloaded.CancelReservation())
	assertState(t, reloaded, StateAvailable)
}

func TestTransactionStoreAdapter(t *testing.T) {
	store := &recordingTransactionStore{}
	now := time.Date(2026, 6, 16, 10, 0, 0, 0, time.UTC)
	c := New("cp-1", 2, WithTransactionStore(store), WithClock(func() time.Time {
		return now
	}))

	assertNoError(t, c.PlugIn())
	assertNoError(t, c.StartTransaction(TransactionBegin{
		ID:         "tx-1",
		IDTag:      "id-tag",
		MeterStart: 12,
		Metadata:   map[string]any{"source": "test"},
	}))

	if store.begin.ID != "tx-1" {
		t.Fatalf("begin transaction ID = %q, want tx-1", store.begin.ID)
	}
	if store.begin.CPID != "cp-1" || store.begin.EVSEID != 2 {
		t.Fatalf("begin connector = %s/%d, want cp-1/2", store.begin.CPID, store.begin.EVSEID)
	}
	if store.begin.Status != storage.TransactionActive {
		t.Fatalf("begin status = %s, want %s", store.begin.Status, storage.TransactionActive)
	}
	if !store.begin.StartedAt.Equal(now) {
		t.Fatalf("begin time = %s, want %s", store.begin.StartedAt, now)
	}
	if store.begin.Metadata["source"] != "test" {
		t.Fatalf("begin metadata = %#v", store.begin.Metadata)
	}

	assertNoError(t, c.StopTransaction(TransactionEnd{MeterStop: 34}))
	if store.endID != "tx-1" {
		t.Fatalf("end transaction ID = %q, want tx-1", store.endID)
	}
	if store.end.MeterStop != 34 {
		t.Fatalf("meter stop = %d, want 34", store.end.MeterStop)
	}
	if store.end.Status != storage.TransactionCompleted {
		t.Fatalf("end status = %s, want %s", store.end.Status, storage.TransactionCompleted)
	}
	if !store.end.EndedAt.Equal(now) {
		t.Fatalf("end time = %s, want %s", store.end.EndedAt, now)
	}
}

func TestTransactionStoreRequiresTransactionID(t *testing.T) {
	c := New("cp-1", 1, WithTransactionStore(&recordingTransactionStore{}))

	assertNoError(t, c.PlugIn())
	err := c.StartTransaction(TransactionBegin{})
	if !errors.Is(err, ErrTransactionIDRequired) {
		t.Fatalf("StartTransaction error = %v, want %v", err, ErrTransactionIDRequired)
	}
	assertState(t, c, StatePreparing)
}

func assertState(t *testing.T, c *Connector, want State) {
	t.Helper()

	if got := c.State(); got != want.String() {
		t.Fatalf("state = %s, want %s", got, want)
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

type recordingTransactionStore struct {
	begin storage.Transaction
	endID string
	end   storage.TransactionEnd
}

func (s *recordingTransactionStore) Begin(ctx context.Context, tx storage.Transaction) error {
	s.begin = tx
	return nil
}

func (s *recordingTransactionStore) Update(ctx context.Context, txID string, mut storage.TransactionMutation) error {
	return nil
}

func (s *recordingTransactionStore) End(ctx context.Context, txID string, end storage.TransactionEnd) error {
	s.endID = txID
	s.end = end
	return nil
}

func (s *recordingTransactionStore) Get(ctx context.Context, txID string) (storage.Transaction, error) {
	if txID == s.begin.ID {
		return s.begin, nil
	}
	return storage.Transaction{}, errors.New("not found")
}

func (s *recordingTransactionStore) ListActive(ctx context.Context, cpID string) ([]storage.Transaction, error) {
	if s.begin.CPID != cpID || !reflect.DeepEqual(s.end, storage.TransactionEnd{}) {
		return nil, nil
	}
	return []storage.Transaction{s.begin}, nil
}
