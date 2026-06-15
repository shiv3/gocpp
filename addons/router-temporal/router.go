package routertemporal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
)

var _ storage.MessageRouter = (*Router)(nil)

var workflowSequence atomic.Uint64

// Router implements storage.MessageRouter with Temporal workflows and
// activities.
type Router struct {
	client    client.Client
	taskQueue string
	registry  storage.ConnectionRegistry
	config    config
}

// New returns a Temporal-backed storage.MessageRouter.
//
// taskQueue is the Temporal task queue for this CSMS instance. By default,
// ConnectionRegistry global instance IDs are treated as Temporal task queue
// names; use WithTaskQueueForInstance when the deployment uses different queue
// names.
func New(c client.Client, taskQueue string, reg storage.ConnectionRegistry, opts ...Option) storage.MessageRouter {
	if c == nil {
		panic("router-temporal: nil Temporal client")
	}
	if taskQueue == "" {
		panic("router-temporal: empty task queue")
	}
	if reg == nil {
		panic("router-temporal: nil connection registry")
	}

	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	return &Router{
		client:    c,
		taskQueue: taskQueue,
		registry:  reg,
		config:    cfg,
	}
}

// CallLocal preserves the core no-op router semantics. Local delivery is owned
// by the dispatcher/connection holder, not this inter-instance router.
func (r *Router) CallLocal(context.Context, string, string, []byte) ([]byte, error) {
	return nil, storage.ErrNotLocal
}

// CallRemote starts a Temporal workflow that forwards the call to the task queue
// of the instance currently recorded as holding cpID.
func (r *Router) CallRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
	instanceID, ok, err := r.registry.LookupGlobal(ctx, cpID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, storage.ErrNotLocal
	}

	targetTaskQueue, ok := r.config.taskQueueForInstance(instanceID)
	if !ok || targetTaskQueue == "" {
		return nil, storage.ErrNotLocal
	}

	input := routeCallWorkflowInput{
		CPID:                     cpID,
		Action:                   action,
		Request:                  cloneBytes(req),
		TargetTaskQueue:          targetTaskQueue,
		ActivityScheduleToClose:  newTimeDuration(r.config.activityScheduleToClose),
		ActivityStartToClose:     newTimeDuration(r.config.activityStartToClose),
		ActivityHeartbeatTimeout: newTimeDuration(r.config.activityHeartbeatTimeout),
		ActivityRetryPolicy:      cloneRetryPolicy(r.config.activityRetryPolicy),
	}
	run, err := r.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                       r.workflowID(cpID, action),
		TaskQueue:                r.taskQueue,
		WorkflowExecutionTimeout: r.config.workflowExecutionTimeout,
		WorkflowRunTimeout:       r.config.workflowRunTimeout,
		WorkflowTaskTimeout:      r.config.workflowTaskTimeout,
	}, routeCallWorkflowName, input)
	if err != nil {
		return nil, mapTemporalError(err)
	}

	var out routeCallActivityOutput
	err = run.Get(ctx, &out)
	if err != nil {
		if r.config.cancelWorkflowOnContextErr && ctx.Err() != nil {
			_ = r.cancelWorkflow(run)
		}
		return nil, mapTemporalError(err)
	}
	return cloneBytes(out.Response), nil
}

// ServeRemote starts a Temporal worker for this router's task queue and runs it
// until ctx is cancelled.
func (r *Router) ServeRemote(ctx context.Context, handler storage.RemoteHandler) error {
	if handler == nil {
		return storage.ErrRouterNotImplemented
	}

	w := worker.New(r.client, r.taskQueue, r.config.workerOptions)
	w.RegisterWorkflowWithOptions(routeCallWorkflow, workflowRegisterOptions())
	w.RegisterActivityWithOptions((&remoteActivities{handler: handler}).deliver, activityRegisterOptions())

	interruptCh := make(chan interface{})
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			close(interruptCh)
		case <-done:
		}
	}()

	err := w.Run(interruptCh)
	close(done)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func (r *Router) workflowID(cpID, action string) string {
	parts := []string{
		sanitizeWorkflowIDPart(r.config.workflowIDPrefix, 80),
		sanitizeWorkflowIDPart(cpID, 96),
		sanitizeWorkflowIDPart(action, 64),
		fmt.Sprintf("%d", time.Now().UnixNano()),
		fmt.Sprintf("%d", workflowSequence.Add(1)),
	}
	return strings.Join(parts, "-")
}

func (r *Router) cancelWorkflow(run client.WorkflowRun) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.client.CancelWorkflow(ctx, run.GetID(), run.GetRunID())
}

func mapTemporalError(err error) error {
	if err == nil {
		return nil
	}

	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		switch appErr.Type() {
		case temporalErrorTypeNotLocal:
			return storage.ErrNotLocal
		case temporalErrorTypeRouterNotImplemented:
			return storage.ErrRouterNotImplemented
		}
	}
	return err
}

func cloneBytes(in []byte) []byte {
	if in == nil {
		return nil
	}
	return append([]byte(nil), in...)
}

func cloneRetryPolicy(in *temporal.RetryPolicy) *temporal.RetryPolicy {
	if in == nil {
		return nil
	}
	out := *in
	out.NonRetryableErrorTypes = append([]string(nil), in.NonRetryableErrorTypes...)
	return &out
}

func sanitizeWorkflowIDPart(in string, max int) string {
	var b strings.Builder
	b.Grow(len(in))
	lastDash := false
	for _, r := range in {
		var ch byte
		switch {
		case r >= 'a' && r <= 'z':
			ch = byte(r)
		case r >= 'A' && r <= 'Z':
			ch = byte(r)
		case r >= '0' && r <= '9':
			ch = byte(r)
		default:
			ch = '-'
		}
		if ch == '-' {
			if b.Len() == 0 || lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteByte(ch)
		if max > 0 && b.Len() >= max {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}
