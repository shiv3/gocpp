package storage

import (
	"context"
	"time"
)

// Session is the registry's record of one live charge point connection.
type Session struct {
	// CPID is the charge point identity.
	CPID string
	// GatewayID names the process (CSMS instance or gateway) that holds the socket.
	GatewayID string
	// Boot identifies one run of that process. A restarted process has a new
	// Boot, which invalidates every session recorded under its old one at once.
	// An empty Boot marks a record written through the legacy
	// ConnectionRegistry.PutGlobal; such a record has no lease to check and is
	// always considered live.
	Boot string
	// Epoch is assigned by the registry on Claim and grows for every claim of
	// the same CPID, so it orders connections of one charge point. Zero means
	// "not claimed" (a session accepted while the registry was unreachable).
	Epoch uint64
	// Version is the negotiated OCPP version.
	Version     string
	ConnectedAt time.Time
	// Meta carries optional connection details (remote address, selected
	// headers, ...). The registry stores it opaquely.
	Meta map[string]string
}

// Gateway lease states.
const (
	GatewayServing  = "serving"
	GatewayDraining = "draining"
)

// GatewayInfo describes a process holding a lease in the registry.
type GatewayInfo struct {
	ID   string
	Boot string
	// Address is where other processes can reach it (optional, informational).
	Address string
	// State is GatewayServing or GatewayDraining.
	State string
	// Sessions is the number of sessions the process currently holds.
	Sessions int
}

// SessionRegistry is the cluster-wide record of which process holds which
// charge point socket. It replaces the global half of ConnectionRegistry: no
// operation is an unconditional write, so a delayed or stale writer can never
// overwrite a newer connection.
//
// Implementations must make Claim, Release and Reassert atomic with respect to
// each other for one CPID. Liveness is decided by the gateway lease: Lookup and
// LookupMany report a session only while its gateway's lease (see Heartbeat)
// exists and carries the same Boot as the record (records with an empty Boot
// excepted).
type SessionRegistry interface {
	// Claim atomically assigns the next epoch for s.CPID and records the caller
	// as owner. s.Epoch is ignored. It returns the new epoch and the session it
	// displaced, if any (regardless of whether the displaced one is live).
	Claim(ctx context.Context, s Session) (epoch uint64, prev *Session, err error)
	// Release removes the record only if it still has this epoch.
	Release(ctx context.Context, cpID string, epoch uint64) (released bool, err error)
	// Reassert restores a record that is absent or older than s.Epoch, and
	// refreshes the record's expiry when it already equals s. It reports false
	// when the registry holds a newer epoch (or a different owner of the same
	// epoch): the caller's socket has been superseded and should be closed.
	Reassert(ctx context.Context, s Session) (ok bool, err error)
	// Lookup returns the live session of cpID.
	Lookup(ctx context.Context, cpID string) (Session, bool, error)
	// LookupMany returns the live sessions among cpIDs; ids without one are
	// omitted. The result order is unspecified.
	LookupMany(ctx context.Context, cpIDs []string) ([]Session, error)

	// Heartbeat creates or renews the lease of one gateway for ttl. One key per
	// gateway, not per charger.
	Heartbeat(ctx context.Context, g GatewayInfo, ttl time.Duration) error
	// Gateways lists the gateways whose lease is current.
	Gateways(ctx context.Context) ([]GatewayInfo, error)
}
