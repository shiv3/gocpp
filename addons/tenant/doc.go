// Package tenant provides multi-tenant partition helpers for gocpp CSMS
// deployments.
//
// The package supports two isolation strategies:
//
//   - Manager creates independent in-memory storage instances per tenant.
//   - Namespaced wrappers partition a shared backing store by prefixing the
//     storage keys used for each tenant.
//
// A common CSMS wiring pattern is to use csms.WithCPIDExtractor to fold the
// tenant and charge point identifiers into one slash-free compound charge point
// id, then pass tenant-scoped stores from Manager.For to csms.WithConnectionRegistry,
// csms.WithTransactionStore, and csms.WithConfigStore. For example, an HTTP path
// like /ocpp/acme/CP_1 can extract tenant "acme" and charge point "CP_1", then
// expose "acme:CP_1" to the CSMS while the selected stores remain scoped to
// tenant "acme".
package tenant
