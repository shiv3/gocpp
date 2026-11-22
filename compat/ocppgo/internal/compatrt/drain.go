// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package compatrt

import (
	"context"
	"errors"
	"math"
	"time"
)

// ErrDrainInProgress is returned when a drain is already running.
var ErrDrainInProgress = errors.New("compat: drain already in progress")

// DrainOptions tunes a drain. Zero values select the defaults.
type DrainOptions struct {
	// Window is the time in which every connection is closed; connections still
	// open when it ends are closed regardless (default 5 minutes).
	Window time.Duration
	// Rate is the number of connections closed per second (default: connections
	// divided by 0.9 x Window).
	Rate float64
	// IdleGrace is how long a busy connection (a request in flight in either
	// direction) is left alone, with no new outbound request started, before it is
	// closed anyway (default 30 s).
	IdleGrace time.Duration
	// RetryAfter is the base of the jittered Retry-After header of the 503 that
	// refuses upgrades while draining (default 10 s).
	RetryAfter time.Duration
	// OnProgress is called about once a second and when the drain ends.
	OnProgress func(DrainProgress)
}

// DrainProgress is a snapshot reported to DrainOptions.OnProgress.
type DrainProgress struct {
	Total, Closed, Forced, Remaining int
	Elapsed                          time.Duration
}

// WithDefaults fills in the zero values for n connections.
func (o DrainOptions) WithDefaults(n int) DrainOptions {
	if o.Window <= 0 {
		o.Window = 5 * time.Minute
	}
	if o.IdleGrace <= 0 {
		o.IdleGrace = 30 * time.Second
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = 10 * time.Second
	}
	if o.Rate <= 0 {
		o.Rate = math.Max(1, float64(n)/(0.9*o.Window.Seconds()))
	}
	return o
}

// DrainTarget is one connection of a drain.
type DrainTarget interface {
	// CloseIfIdle closes the connection if nothing is in flight, reporting whether it did.
	CloseIfIdle() bool
	// Quiesce stops it from starting new outbound requests.
	Quiesce()
	// Unquiesce takes Quiesce back.
	Unquiesce()
	// ForceClose closes it whatever it is doing.
	ForceClose()
	// Done is closed when the connection is gone.
	Done() <-chan struct{}
}

type busyTarget struct {
	t        DrainTarget
	deadline time.Time
}

// RunDrain closes the targets at opts.Rate (idle ones first) within opts.Window
// and returns when all of them are gone, or with ctx.Err(). opts must have its
// defaults applied.
func RunDrain(ctx context.Context, targets []DrainTarget, opts DrainOptions) error {
	start := time.Now()
	deadline := start.Add(opts.Window)
	total := len(targets)
	queue := targets
	var busy []busyTarget
	closed, forced := 0, 0
	var closedTargets []DrainTarget
	capTokens := math.Max(1, opts.Rate/5)
	tokens := 0.0
	last := start
	var lastReport time.Time
	report := func(now time.Time, final bool) {
		if opts.OnProgress == nil || (!final && now.Sub(lastReport) < time.Second) {
			return
		}
		lastReport = now
		opts.OnProgress(DrainProgress{Total: total, Closed: closed, Forced: forced, Remaining: total - closed, Elapsed: now.Sub(start)})
	}
	closeOne := func(t DrainTarget, force bool) {
		if force {
			t.ForceClose()
			forced++
		}
		closed++
		closedTargets = append(closedTargets, t)
	}
	gone := func(t DrainTarget) bool {
		select {
		case <-t.Done():
			total--
			return true
		default:
			return false
		}
	}
	pick := func(now time.Time) bool {
		for len(queue) > 0 {
			t := queue[0]
			queue = queue[1:]
			if gone(t) {
				continue
			}
			if t.CloseIfIdle() {
				closeOne(t, false)
				return true
			}
			t.Quiesce()
			busy = append(busy, busyTarget{t, now.Add(opts.IdleGrace)})
		}
		for i := 0; i < len(busy); i++ {
			b := busy[i]
			if gone(b.t) {
				busy = append(busy[:i], busy[i+1:]...)
				i--
				continue
			}
			switch {
			case b.t.CloseIfIdle():
				closeOne(b.t, false)
			case !now.Before(b.deadline):
				closeOne(b.t, true)
			default:
				continue
			}
			busy = append(busy[:i], busy[i+1:]...)
			return true
		}
		return false
	}
	defer func() {
		for _, b := range busy {
			b.t.Unquiesce()
		}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for len(queue) > 0 || len(busy) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-ticker.C:
			if !now.Before(deadline) {
				for _, t := range queue {
					closeOne(t, true)
				}
				for _, b := range busy {
					closeOne(b.t, true)
				}
				queue, busy = nil, nil
				break
			}
			tokens = math.Min(capTokens, tokens+opts.Rate*now.Sub(last).Seconds())
			last = now
			for tokens >= 1 && pick(now) {
				tokens--
			}
			report(now, false)
		}
	}
	for _, t := range closedTargets {
		select {
		case <-t.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	report(time.Now(), true)
	return nil
}
