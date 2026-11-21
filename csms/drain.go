package csms

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Defaults of DrainOptions.
const (
	defaultDrainWindow     = 5 * time.Minute
	defaultDrainIdleGrace  = 30 * time.Second
	defaultDrainRetryAfter = 10 * time.Second
	// drainTick is how often the pacer wakes up; tokens accumulate in between,
	// so the rate does not depend on it.
	drainTick = 10 * time.Millisecond
)

// ErrDrainInProgress is returned by Drain when a drain is already running.
var ErrDrainInProgress = errors.New("csms: drain already in progress")

// DrainOptions tunes Server.Drain. Zero values select the defaults.
type DrainOptions struct {
	// Window is the time in which every session is closed, and the hard limit:
	// sessions still open when it ends are closed regardless of what they are
	// doing (default 5 minutes).
	Window time.Duration
	// Rate is the number of sessions closed per second. Zero means
	// sessions / (0.9 x Window) for the sessions present when Drain starts, so
	// 10,000 chargers and a 5 minute window leave at about 37 per second and the
	// last tenth of the window is slack for sessions that were busy.
	Rate float64
	// IdleGrace is how long a session that is busy (a CALL in flight or queued in
	// either direction) when its turn comes is left alone, retrying every tick,
	// before it is closed anyway (default 30s, one call timeout). Idle sessions
	// are always closed first.
	IdleGrace time.Duration
	// RetryAfter is the base of the jittered Retry-After header (seconds) sent
	// with the 503 that refuses new upgrades while draining (default 10s; the
	// value is spread over 0.5x to 1.5x of it).
	RetryAfter time.Duration
	// OnProgress, if set, is called about once a second and when the drain ends.
	OnProgress func(DrainProgress)
}

// DrainProgress is a snapshot reported by DrainOptions.OnProgress.
type DrainProgress struct {
	// Total is the number of sessions the drain started with.
	Total int
	// Closed is the number closed so far, Forced of them after their grace or the
	// window ran out while busy.
	Closed, Forced int
	// Remaining is Total - Closed.
	Remaining int
	Elapsed   time.Duration
}

func (o DrainOptions) withDefaults(sessions int) DrainOptions {
	if o.Window <= 0 {
		o.Window = defaultDrainWindow
	}
	if o.IdleGrace <= 0 {
		o.IdleGrace = defaultDrainIdleGrace
	}
	if o.RetryAfter <= 0 {
		o.RetryAfter = defaultDrainRetryAfter
	}
	if o.Rate <= 0 {
		// Pace to 90% of the window: the rest is slack for sessions passed over
		// while busy, so the window's end does not force the tail.
		o.Rate = float64(max(sessions, 1)) / (0.9 * o.Window.Seconds())
	}
	return o
}

// Draining reports whether Drain has been called. A readiness probe should fail
// while it is true so the load balancer stops sending upgrades.
func (s *Server) Draining() bool { return s.draining.Load() }

// ReadyHandler returns a readiness handler: 200 while the server accepts new
// connections, 503 once Drain or Shutdown has begun.
func (s *Server) ReadyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if s.draining.Load() || s.stopping.Load() || s.ctx.Err() != nil {
			http.Error(w, "draining", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
}

// AdmissionStats counts upgrades refused before they were accepted.
type AdmissionStats struct {
	// RejectedRateLimit were refused by WithAcceptRateLimit, RejectedDraining while
	// draining.
	RejectedRateLimit, RejectedDraining uint64
}

// AdmissionStats returns the refusal counters.
func (s *Server) AdmissionStats() AdmissionStats {
	return AdmissionStats{RejectedRateLimit: s.acceptRejected.Load(), RejectedDraining: s.drainRejected.Load()}
}

// Drain moves every charge point off this server without a reconnect storm,
// for a deploy or a planned shutdown of a process that holds many sockets.
//
//  1. New upgrades are refused with 503 and a jittered Retry-After (Draining
//     reports true, so readiness checks fail), and the gateway lease, if a session
//     registry is in use, is marked draining.
//  2. Sessions are closed at a fixed rate (DrainOptions.Rate, by default sessions
//     divided by the window), idle ones first: a session with a CALL in flight or
//     queued in either direction is passed over; when its turn comes it stops
//     accepting new calls, which fail at once with ocppj.ErrGatewayDraining
//     before anything is written (safe to retry elsewhere), and it is closed as
//     soon as what is in flight has finished, or when its grace (IdleGrace) has
//     run out.
//  3. The close frame carries code 1012 (service restart) and the disconnect
//     callback receives ocppj.ErrGatewayDraining as the cause. WebSocket close
//     frames cannot carry a retry delay, so the spread of reconnects is the
//     pace of the closes; configure the chargers' own retry back-off where the
//     protocol allows.
//  4. When the window ends the sessions left are closed regardless; calls in
//     flight then fail with ocppj.ErrDrainTimeout and may have been executed.
//
// Drain returns once every session has been closed and torn down, or with
// ctx.Err() if ctx ends first (sessions not yet closed stay open). It does not
// stop the HTTP server or the Server; call Shutdown afterwards. Only one Drain
// can run at a time (ErrDrainInProgress).
func (s *Server) Drain(ctx context.Context, opts DrainOptions) error {
	if !s.draining.CompareAndSwap(false, true) {
		return ErrDrainInProgress
	}
	var targets []drainTarget
	s.mu.RLock()
	opts = opts.withDefaults(len(s.conns))
	s.drainRetryAfter.Store(int64(opts.RetryAfter))
	for _, c := range s.conns {
		targets = append(targets, connTarget{c})
	}
	s.mu.RUnlock()
	s.cfg.dispatcher.Logger.Info("csms: drain started", "sessions", len(targets),
		"window", opts.Window, "rate_per_s", opts.Rate)
	if s.sessions != nil {
		s.sessions.draining.Store(true)
		s.sessions.heartbeat(ctx)
	}
	err := runDrain(ctx, targets, opts)
	s.cfg.dispatcher.Logger.Info("csms: drain finished", "error", err)
	return err
}

// drainTarget is one session as the drain pacer sees it.
type drainTarget interface {
	// closeIfIdle closes the session if nothing is in flight, reporting whether it
	// did.
	closeIfIdle() bool
	// quiesce stops it from starting new calls, so what is in flight can finish.
	quiesce()
	// forceClose closes it whatever it is doing.
	forceClose()
	// done is closed when the session is gone (torn down).
	done() <-chan struct{}
}

type busyTarget struct {
	t        drainTarget
	deadline time.Time
}

// runDrain closes targets at opts.Rate, idle first, within opts.Window.
func runDrain(ctx context.Context, targets []drainTarget, opts DrainOptions) error {
	start := time.Now()
	deadline := start.Add(opts.Window)
	total := len(targets)
	queue := targets
	var busy []busyTarget
	closed, forced := 0, 0
	var closedTargets []drainTarget
	// Burst is capped at 200ms of tokens so a stretch where everyone was busy
	// does not turn into a spike of closes afterwards.
	capTokens := math.Max(1, opts.Rate/5)
	tokens := 0.0
	last := start
	var lastReport time.Time

	report := func(now time.Time, final bool) {
		if opts.OnProgress == nil || (!final && now.Sub(lastReport) < time.Second) {
			return
		}
		lastReport = now
		opts.OnProgress(DrainProgress{Total: total, Closed: closed, Forced: forced,
			Remaining: total - closed, Elapsed: now.Sub(start)})
	}
	closeOne := func(t drainTarget, force bool) {
		if force {
			t.forceClose()
			forced++
		}
		closed++
		closedTargets = append(closedTargets, t)
	}
	// pick returns the next session to close within the rate: an idle one from the
	// queue first (busy ones are parked with their grace deadline), then a parked
	// one that became idle or ran out of grace. It reports false when nothing is
	// eligible right now.
	pick := func(now time.Time) bool {
		for len(queue) > 0 {
			t := queue[0]
			queue = queue[1:]
			select {
			case <-t.done():
				total-- // went away by itself
				continue
			default:
			}
			if t.closeIfIdle() {
				closeOne(t, false)
				return true
			}
			// Its turn has come but it is mid-call: let that call finish and start no
			// new ones (they fail unsent and the caller retries elsewhere), so a
			// session under constant traffic still becomes idle.
			t.quiesce()
			busy = append(busy, busyTarget{t, now.Add(opts.IdleGrace)})
		}
		for i := 0; i < len(busy); i++ {
			b := busy[i]
			select {
			case <-b.t.done():
				total--
				busy = append(busy[:i], busy[i+1:]...)
				i--
				continue
			default:
			}
			switch {
			case b.t.closeIfIdle():
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

	ticker := time.NewTicker(drainTick)
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
	// Every close has been initiated; wait for the teardowns.
	for _, t := range closedTargets {
		select {
		case <-t.done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	report(time.Now(), true)
	return nil
}

// connTarget adapts a *Conn.
type connTarget struct{ c *Conn }

func (t connTarget) closeIfIdle() bool     { return t.c.inner.TryDrain() }
func (t connTarget) quiesce()              { t.c.inner.Quiesce() }
func (t connTarget) forceClose()           { t.c.inner.ForceDrain() }
func (t connTarget) done() <-chan struct{} { return t.c.gone }

// jitterRetryAfter renders a Retry-After value spread over 0.5x to 1.5x of base,
// at least one second.
func jitterRetryAfter(base time.Duration) string {
	secs := base.Seconds() * (0.5 + rand.Float64())
	return strconv.Itoa(max(1, int(math.Round(secs))))
}

// tokenBucket is a mutex-guarded token bucket.
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	b := math.Max(1, float64(burst))
	return &tokenBucket{rate: rate, burst: b, tokens: b}
}

func (b *tokenBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() {
		b.tokens = math.Min(b.burst, b.tokens+b.rate*now.Sub(b.last).Seconds())
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// refillTime is how long an empty bucket takes to fill up.
func (b *tokenBucket) refillTime() time.Duration {
	return time.Duration(b.burst / b.rate * float64(time.Second))
}

// WithAcceptRateLimit limits how fast new WebSocket upgrades are admitted: a token
// bucket of rate upgrades per second with the given burst. An upgrade over the
// limit is answered with 503 and a jittered Retry-After before authentication or
// any other work, so a reconnect storm after a crash or restart cannot turn into
// a CPU, TLS or database storm. Combine with WithMaxConnections (the capacity
// cap) and, to also shed TLS handshakes, wrap the listener with
// RateLimitListener. rate <= 0 disables the limit.
func WithAcceptRateLimit(rate float64, burst int) Option {
	return optionFunc(func(c *serverConfig) {
		if rate > 0 {
			c.acceptLimit = newTokenBucket(rate, burst)
		}
	})
}

// RateLimitListener wraps l so that connections accepted faster than rate per
// second (after an initial burst) are closed immediately, before any TLS
// handshake or HTTP parsing: the cheapest place to shed a reconnect storm.
// Chargers see a failed connect and retry with their own back-off, so prefer
// WithAcceptRateLimit (a 503 with Retry-After) where the load balancer
// terminates TLS and the cost is only the upgrade.
func RateLimitListener(l net.Listener, rate float64, burst int) net.Listener {
	if rate <= 0 {
		return l
	}
	return &limitListener{Listener: l, b: newTokenBucket(rate, burst)}
}

type limitListener struct {
	net.Listener
	b *tokenBucket
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.b.allow(time.Now()) {
			return c, nil
		}
		_ = c.Close()
	}
}
