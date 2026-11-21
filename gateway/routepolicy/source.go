package routepolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

// Source delivers policy documents to an Engine. Watch calls apply with the
// current document and then with every newer one until ctx ends. rev is the
// source's revision (0 = use the document's own); apply returns an error for a
// document it refuses, and the previous policy stays in force.
type Source interface {
	Watch(ctx context.Context, apply func(rev uint64, data []byte) error) error
}

// Watch runs src against the engine until ctx ends. Refused documents are
// logged; they never stop the watch.
func (e *Engine) Watch(ctx context.Context, src Source) error {
	return src.Watch(ctx, func(rev uint64, data []byte) error {
		err := e.ApplyData(rev, data)
		switch {
		case err == nil:
			e.log.Info("routepolicy: policy applied", "revision", e.Revision())
		case errors.Is(err, ErrStaleRevision):
			return nil
		default:
			e.log.Error("routepolicy: policy rejected, keeping the previous one", "revision", rev, "error", err)
		}
		return err
	})
}

// FileSource watches a policy file: it is re-read when its modification time or
// size changes (checked every second) and in any case every Interval (default
// 30 s), which also covers ConfigMap symlink swaps. A missing or unreadable
// file keeps the last policy.
type FileSource struct {
	Path     string
	Interval time.Duration
}

// Watch implements Source.
func (f FileSource) Watch(ctx context.Context, apply func(uint64, []byte) error) error {
	every := f.Interval
	if every <= 0 {
		every = 30 * time.Second
	}
	var (
		last     []byte
		sig      string
		lastRead time.Time
	)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		fi, err := os.Stat(f.Path)
		if err == nil {
			cur := fmt.Sprint(fi.ModTime().UnixNano(), fi.Size())
			if cur != sig || time.Since(lastRead) >= every {
				sig, lastRead = cur, time.Now()
				if data, err := os.ReadFile(f.Path); err == nil && string(data) != string(last) {
					last = data
					_ = apply(0, data)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
}

// StoreSource reads the policy from a storage.PolicyStore (the session
// registry) with push propagation.
type StoreSource struct{ Store storage.PolicyStore }

// Watch implements Source.
func (s StoreSource) Watch(ctx context.Context, apply func(uint64, []byte) error) error {
	for {
		err := s.Store.WatchPolicy(ctx, func(rev uint64, data []byte) { _ = apply(rev, data) })
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// The store dropped the watch (outage): the last policy stays in force;
		// resubscribe after a pause.
		_ = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// Describe is a one-line summary of a policy for logs and the CLI.
func Describe(p Policy) string {
	return fmt.Sprintf("revision %d, default %q, %d rule(s)", p.Revision, p.DefaultPool(), len(p.Rules))
}
