# gocpp tenant addon

Package `tenant` provides helpers for partitioning gocpp CSMS storage by tenant.

## API

- `type Resolver func(*http.Request) (tenantID string, ok bool)` extracts a tenant id from an HTTP request.
- `FromPathPrefix(index int)` extracts a zero-based path segment, for example index `1` from `/ocpp/acme/CP_1`.
- `FromHeader(name string)` extracts a tenant id from a request header.
- `NewManager()` returns a manager that lazily creates independent in-memory `ConnectionRegistry`, `TransactionStore`, and `ConfigStore` instances per tenant.
- `NewNamespacedConnectionRegistry`, `NewNamespacedTransactionStore`, and `NewNamespacedConfigStore` wrap shared backing stores by prefixing tenant-owned keys.

Use `Manager.For` when each tenant should get independent in-memory stores. Use the namespaced wrappers when multiple tenant views should share one backing store while keeping their public ids tenant-local.

## CSMS wiring

For a path shaped like `/ocpp/{tenant}/{cpID}`, use `WithCPIDExtractor` to return one slash-free compound charge point id to the CSMS, then provide stores scoped to the tenant being served.

```go
package main

import (
	"net/http"

	"github.com/shiv3/gocpp/addons/tenant"
	"github.com/shiv3/gocpp/csms"
)

func newTenantServer(manager *tenant.Manager, tenantID string) *csms.Server {
	connReg, txStore, cfgStore := manager.For(tenantID)
	cpFromPath := tenant.FromPathPrefix(2)

	return csms.NewServer(
		csms.WithCPIDExtractor(func(r *http.Request) (string, bool) {
			cpID, ok := cpFromPath(r)
			if !ok {
				return "", false
			}
			return tenantID + ":" + cpID, true
		}),
		csms.WithConnectionRegistry(connReg),
		csms.WithTransactionStore(txStore),
		csms.WithConfigStore(cfgStore),
	)
}
```

For a shared backing store, wrap it once per tenant:

```go
import "github.com/shiv3/gocpp/core/storage/memory"

baseReg := memory.NewConnectionRegistry()
baseTx := memory.NewTransactionStore()
baseCfg := memory.NewConfigStore()

connReg := tenant.NewNamespacedConnectionRegistry("acme", baseReg)
txStore := tenant.NewNamespacedTransactionStore("acme", baseTx)
cfgStore := tenant.NewNamespacedConfigStore("acme", baseCfg)
```
