//go:build !short

package chaos

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/shiv3/gocpp/addons/rollout"
	"github.com/stretchr/testify/require"
)

const e2ePlan = `
rule: step
base:
  rules:
    - {name: step, pool: canary}
stages:
  - {name: all, percent: 100, soak: 5s}
interval: 500ms
window: 2s
gates: {min_requests: 20}
`

// The automatic promotion controller, with the gateways' own /metrics as its
// source, rolls back an error-injecting canary by itself, within about one
// evaluation window; a healthy canary advances to the end of the plan.
func TestChaos_AutomaticRolloutRollsBackAnErrorInjectingCanary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		extra    []string
		rollback bool
	}{{"error injecting", []string{"-fail-heartbeat"}, true}, {"healthy", nil, false}} {
		t.Run(tc.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			g := startGateway(t, "gw-1", mr.Addr())
			startPoolBackend(t, "be-d", "", mr.Addr(), g.link)
			startPoolBackend(t, "be-c", "canary", mr.Addr(), g.link, tc.extra...)
			require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
			pc := controller(t, mr.Addr())
			waitLeases(t, pc)

			// Traffic: three chargers heartbeat continuously.
			stop := make(chan struct{})
			var ok, failed atomic.Int64
			// converged is set once every gateway runs the policy the rollback
			// wrote. A call that started before that may still be routed to (or be
			// in flight on) the canary: that is the documented propagation window,
			// so only calls that started after it count as failures after the
			// rollback.
			var converged atomic.Bool
			var okAfter, failedAfter atomic.Int64
			for _, id := range []string{"CP_1", "CP_2", "CP_3"} {
				c := newCharger(t, g.ws, id)
				go func() {
					for {
						select {
						case <-stop:
							return
						case <-time.After(25 * time.Millisecond):
						}
						after := converged.Load()
						ctx, cancel := ctxT(5 * time.Second)
						if err := c.heartbeat(ctx); err != nil {
							failed.Add(1)
							if after {
								failedAfter.Add(1)
							}
						} else {
							ok.Add(1)
							if after {
								okAfter.Add(1)
							}
						}
						cancel()
					}
				}()
			}
			defer close(stop)

			plan, err := rollout.Parse([]byte(e2ePlan))
			require.NoError(t, err)
			src := &rollout.ScrapeSource{Targets: []string{"http://" + g.admin + "/metrics"}}
			var decisions []rollout.Decision
			ctl := rollout.New(plan, pc, src, rollout.NotifyFunc(func(d rollout.Decision) { decisions = append(decisions, d) }))
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			start := time.Now()
			require.NoError(t, ctl.Run(ctx))

			if tc.rollback {
				require.Equal(t, rollout.StateRolledBack, ctl.State(), "%+v", decisions)
				require.Less(t, time.Since(start), 12*time.Second, "rolled back within a few evaluation windows of the stage starting")
				cur, err := pc.Current(context.Background())
				require.NoError(t, err)
				require.Empty(t, cur.Rules)
				// after the rollback the default pool answers everything again
				_, err = pc.WaitConverged(context.Background(), cur.Revision, 10*time.Second)
				require.NoError(t, err)
				converged.Store(true)
				require.Eventually(t, func() bool { return okAfter.Load() >= 10 }, 10*time.Second, 20*time.Millisecond)
				require.Zero(t, failedAfter.Load(), "no more errors once the rollback has propagated")
			} else {
				require.Equal(t, rollout.StateDone, ctl.State(), "%+v", decisions)
				cur, err := pc.Current(context.Background())
				require.NoError(t, err)
				require.Len(t, cur.Rules, 1)
				require.Zero(t, failed.Load())
			}
		})
	}
}
