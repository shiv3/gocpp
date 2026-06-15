package tenant

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
)

type fakeConn struct {
	id     string
	closed bool
}

func (f *fakeConn) ID() string {
	return f.id
}

func (f *fakeConn) Close(error) error {
	f.closed = true
	return nil
}

func TestResolvers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ocpp/acme/CP_1", nil)
	req.Header.Set("X-Tenant-ID", " acme ")

	tenantID, ok := FromPathPrefix(1)(req)
	if !ok || tenantID != "acme" {
		t.Fatalf("FromPathPrefix(1) = %q, %v; want acme, true", tenantID, ok)
	}

	tenantID, ok = FromHeader("X-Tenant-ID")(req)
	if !ok || tenantID != "acme" {
		t.Fatalf("FromHeader = %q, %v; want acme, true", tenantID, ok)
	}

	if tenantID, ok = FromPathPrefix(5)(req); ok || tenantID != "" {
		t.Fatalf("FromPathPrefix(5) = %q, %v; want empty, false", tenantID, ok)
	}
}

func TestManagerConnectionRegistryPartitionsSameCPID(t *testing.T) {
	ctx := context.Background()
	manager := NewManager()

	acmeReg, _, _ := manager.For("acme")
	globexReg, _, _ := manager.For("globex")

	acmeConn := &fakeConn{id: "acme-conn"}
	globexConn := &fakeConn{id: "globex-conn"}

	must(t, acmeReg.PutLocal(ctx, "CP_1", acmeConn))
	must(t, globexReg.PutLocal(ctx, "CP_1", globexConn))

	got, ok := acmeReg.GetLocal("CP_1")
	if !ok || got.ID() != "acme-conn" {
		t.Fatalf("acme GetLocal = %v, %v; want acme-conn, true", connID(got), ok)
	}
	got, ok = globexReg.GetLocal("CP_1")
	if !ok || got.ID() != "globex-conn" {
		t.Fatalf("globex GetLocal = %v, %v; want globex-conn, true", connID(got), ok)
	}
}

func TestNamespacedConnectionRegistryPartitionsSameCPID(t *testing.T) {
	ctx := context.Background()
	backing := memory.NewConnectionRegistry()
	acmeReg := NewNamespacedConnectionRegistry("acme", backing)
	globexReg := NewNamespacedConnectionRegistry("globex", backing)

	must(t, acmeReg.PutLocal(ctx, "CP_1", &fakeConn{id: "acme-conn"}))
	must(t, globexReg.PutLocal(ctx, "CP_1", &fakeConn{id: "globex-conn"}))

	got, ok := acmeReg.GetLocal("CP_1")
	if !ok || got.ID() != "acme-conn" {
		t.Fatalf("acme GetLocal = %v, %v; want acme-conn, true", connID(got), ok)
	}
	got, ok = globexReg.GetLocal("CP_1")
	if !ok || got.ID() != "globex-conn" {
		t.Fatalf("globex GetLocal = %v, %v; want globex-conn, true", connID(got), ok)
	}

	var acmeIDs []string
	acmeReg.RangeLocal(func(cpID string, conn storage.LiveConn) bool {
		acmeIDs = append(acmeIDs, cpID)
		return true
	})
	sort.Strings(acmeIDs)
	if got, want := len(acmeIDs), 1; got != want {
		t.Fatalf("acme RangeLocal count = %d; want %d: %v", got, want, acmeIDs)
	}
	if acmeIDs[0] != "CP_1" {
		t.Fatalf("acme RangeLocal id = %q; want CP_1", acmeIDs[0])
	}
}

func TestNamespacedTransactionStorePartitionsSameTxID(t *testing.T) {
	ctx := context.Background()
	backing := memory.NewTransactionStore()
	acmeStore := NewNamespacedTransactionStore("acme", backing)
	globexStore := NewNamespacedTransactionStore("globex", backing)
	now := time.Now()

	must(t, acmeStore.Begin(ctx, storage.Transaction{
		ID:         "tx-1",
		CPID:       "CP_1",
		StartedAt:  now,
		MeterStart: 10,
		Status:     storage.TransactionActive,
	}))
	must(t, globexStore.Begin(ctx, storage.Transaction{
		ID:         "tx-1",
		CPID:       "CP_1",
		StartedAt:  now,
		MeterStart: 20,
		Status:     storage.TransactionActive,
	}))

	acmeTx, err := acmeStore.Get(ctx, "tx-1")
	must(t, err)
	globexTx, err := globexStore.Get(ctx, "tx-1")
	must(t, err)

	if acmeTx.ID != "tx-1" || acmeTx.CPID != "CP_1" || acmeTx.MeterStart != 10 {
		t.Fatalf("acme tx = %+v; want unprefixed tx-1/CP_1 with meter 10", acmeTx)
	}
	if globexTx.ID != "tx-1" || globexTx.CPID != "CP_1" || globexTx.MeterStart != 20 {
		t.Fatalf("globex tx = %+v; want unprefixed tx-1/CP_1 with meter 20", globexTx)
	}

	active, err := acmeStore.ListActive(ctx, "CP_1")
	must(t, err)
	if got, want := len(active), 1; got != want {
		t.Fatalf("acme active count = %d; want %d: %+v", got, want, active)
	}
	if active[0].MeterStart != 10 || active[0].ID != "tx-1" || active[0].CPID != "CP_1" {
		t.Fatalf("acme active = %+v; want tenant-local transaction", active[0])
	}

	must(t, acmeStore.End(ctx, "tx-1", storage.TransactionEnd{
		EndedAt:   now.Add(time.Minute),
		MeterStop: 30,
		Status:    storage.TransactionCompleted,
	}))

	active, err = acmeStore.ListActive(ctx, "CP_1")
	must(t, err)
	if len(active) != 0 {
		t.Fatalf("acme active after End = %+v; want none", active)
	}
	active, err = globexStore.ListActive(ctx, "CP_1")
	must(t, err)
	if got, want := len(active), 1; got != want {
		t.Fatalf("globex active count = %d; want %d: %+v", got, want, active)
	}
}

func TestNamespacedConfigStorePartitionsSameCPIDAndKey(t *testing.T) {
	ctx := context.Background()
	backing := memory.NewConfigStore()
	acmeStore := NewNamespacedConfigStore("acme", backing)
	globexStore := NewNamespacedConfigStore("globex", backing)

	must(t, acmeStore.Put(ctx, "CP_1", "HeartbeatInterval", "60"))
	must(t, globexStore.Put(ctx, "CP_1", "HeartbeatInterval", "120"))

	value, ok, err := acmeStore.Get(ctx, "CP_1", "HeartbeatInterval")
	must(t, err)
	if !ok || value != "60" {
		t.Fatalf("acme Get = %q, %v; want 60, true", value, ok)
	}

	value, ok, err = globexStore.Get(ctx, "CP_1", "HeartbeatInterval")
	must(t, err)
	if !ok || value != "120" {
		t.Fatalf("globex Get = %q, %v; want 120, true", value, ok)
	}

	list, err := acmeStore.List(ctx, "CP_1")
	must(t, err)
	if got, want := list["HeartbeatInterval"], "60"; got != want {
		t.Fatalf("acme List HeartbeatInterval = %q; want %q", got, want)
	}

	must(t, acmeStore.Delete(ctx, "CP_1", "HeartbeatInterval"))
	if _, ok, err = acmeStore.Get(ctx, "CP_1", "HeartbeatInterval"); err != nil || ok {
		t.Fatalf("acme Get after Delete ok=%v err=%v; want false, nil", ok, err)
	}
	value, ok, err = globexStore.Get(ctx, "CP_1", "HeartbeatInterval")
	must(t, err)
	if !ok || value != "120" {
		t.Fatalf("globex Get after acme Delete = %q, %v; want 120, true", value, ok)
	}
}

func TestManagerPartitionsTransactionsAndConfig(t *testing.T) {
	ctx := context.Background()
	manager := NewManager()
	_, acmeTx, acmeCfg := manager.For("acme")
	_, globexTx, globexCfg := manager.For("globex")

	now := time.Now()
	must(t, acmeTx.Begin(ctx, storage.Transaction{ID: "tx-1", CPID: "CP_1", StartedAt: now, MeterStart: 10, Status: storage.TransactionActive}))
	must(t, globexTx.Begin(ctx, storage.Transaction{ID: "tx-1", CPID: "CP_1", StartedAt: now, MeterStart: 20, Status: storage.TransactionActive}))

	acmeGot, err := acmeTx.Get(ctx, "tx-1")
	must(t, err)
	globexGot, err := globexTx.Get(ctx, "tx-1")
	must(t, err)
	if acmeGot.MeterStart != 10 || globexGot.MeterStart != 20 {
		t.Fatalf("manager transactions collided: acme=%+v globex=%+v", acmeGot, globexGot)
	}

	must(t, acmeCfg.Put(ctx, "CP_1", "Key", "acme"))
	must(t, globexCfg.Put(ctx, "CP_1", "Key", "globex"))
	acmeValue, ok, err := acmeCfg.Get(ctx, "CP_1", "Key")
	must(t, err)
	if !ok || acmeValue != "acme" {
		t.Fatalf("acme config = %q, %v; want acme, true", acmeValue, ok)
	}
	globexValue, ok, err := globexCfg.Get(ctx, "CP_1", "Key")
	must(t, err)
	if !ok || globexValue != "globex" {
		t.Fatalf("globex config = %q, %v; want globex, true", globexValue, ok)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func connID(conn storage.LiveConn) string {
	if conn == nil {
		return "<nil>"
	}
	return conn.ID()
}
