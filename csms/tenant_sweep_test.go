package csms

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/stretchr/testify/require"
)

// The tenant table does not keep one entry per tenant id ever seen: idle tenants
// (no session, nothing in flight, bucket refilled) are dropped when it grows,
// tenants with a session are kept, and metric labels stay bounded.
func TestTenantsDropIdleTenants(t *testing.T) {
	tt := newTenants(TenantLimits{MaxMetricTenants: 5, Default: TenantLimit{MaxSessions: 2}}, observability.NoOp{})
	var releases []func()
	_, rel, ok := tt.admit("keeper", false)
	require.True(t, ok)
	releases = append(releases, rel)
	for i := 0; i < 5000; i++ {
		_, rel, ok := tt.admit("t"+strconv.Itoa(i), false)
		require.True(t, ok)
		rel() // the tenant has no session again
	}
	tt.mu.Lock()
	n := len(tt.byID)
	_, kept := tt.byID["keeper"]
	labels := len(tt.labels)
	tt.mu.Unlock()
	require.Less(t, n, 2*tenantSweepMin+1, "idle tenants are evicted")
	require.True(t, kept, "a tenant with a session stays")
	require.LessOrEqual(t, labels, 5, "label cardinality stays bounded")

	// the kept tenant's limit is still the same object: a second session counts
	s, rel2, ok := tt.admit("keeper", false)
	require.True(t, ok)
	require.EqualValues(t, 2, s.sessions.Load())
	_, _, ok = tt.admit("keeper", false)
	require.False(t, ok, "MaxSessions still applies")
	rel2()
	for _, r := range releases {
		r()
	}
}

// A tenant whose message bucket has not refilled is kept, so a reconnect cannot
// reset its budget.
func TestTenantsKeepDrainedBuckets(t *testing.T) {
	tt := newTenants(TenantLimits{Default: TenantLimit{MessageRate: 1, MessageBurst: 10}}, observability.NoOp{})
	s, rel, ok := tt.admit("noisy", false)
	require.True(t, ok)
	rel()
	now := time.Now()
	s.mu.Lock()
	s.tokens, s.last = 0, now
	s.mu.Unlock()
	tt.mu.Lock()
	tt.sweepLocked(now.Add(time.Second))
	_, kept := tt.byID["noisy"]
	tt.sweepLocked(now.Add(11 * time.Second))
	_, keptLater := tt.byID["noisy"]
	tt.mu.Unlock()
	require.True(t, kept)
	require.False(t, keptLater)
}

// Admission racing the sweep never splits one tenant over two states.
func TestTenantsAdmitRacesSweep(t *testing.T) {
	tt := newTenants(TenantLimits{Default: TenantLimit{MaxSessions: 1}}, observability.NoOp{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				_, rel, ok := tt.admit("hot", false)
				if ok {
					rel()
				}
				tt.mu.Lock()
				tt.sweepLocked(time.Now())
				tt.mu.Unlock()
			}
		}()
	}
	wg.Wait()
	s, rel, ok := tt.admit("hot", false)
	require.True(t, ok)
	require.EqualValues(t, 1, s.sessions.Load())
	rel()
}
