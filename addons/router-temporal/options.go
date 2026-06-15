package routertemporal

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
)

const (
	defaultWorkflowExecutionTimeout = 2 * time.Minute
	defaultWorkflowRunTimeout       = 2 * time.Minute
	defaultWorkflowTaskTimeout      = 10 * time.Second
	defaultActivityScheduleToClose  = 90 * time.Second
	defaultActivityStartToClose     = 30 * time.Second
	defaultActivityHeartbeat        = 10 * time.Second
	defaultRetryInitialInterval     = 500 * time.Millisecond
	defaultRetryMaximumInterval     = 5 * time.Second
	defaultRetryMaximumAttempts     = int32(5)
	defaultWorkflowIDPrefix         = "gocpp-router-temporal"
)

// TaskQueueForInstance maps a ConnectionRegistry instance ID to the Temporal
// task queue polled by that instance.
type TaskQueueForInstance func(instanceID string) (taskQueue string, ok bool)

// Option configures the Temporal-backed router.
type Option func(*config)

type config struct {
	taskQueueForInstance       TaskQueueForInstance
	workflowExecutionTimeout   time.Duration
	workflowRunTimeout         time.Duration
	workflowTaskTimeout        time.Duration
	activityScheduleToClose    time.Duration
	activityStartToClose       time.Duration
	activityHeartbeatTimeout   time.Duration
	activityRetryPolicy        *temporal.RetryPolicy
	workerOptions              worker.Options
	workflowIDPrefix           string
	cancelWorkflowOnContextErr bool
}

func defaultConfig() config {
	return config{
		taskQueueForInstance:     defaultTaskQueueForInstance,
		workflowExecutionTimeout: defaultWorkflowExecutionTimeout,
		workflowRunTimeout:       defaultWorkflowRunTimeout,
		workflowTaskTimeout:      defaultWorkflowTaskTimeout,
		activityScheduleToClose:  defaultActivityScheduleToClose,
		activityStartToClose:     defaultActivityStartToClose,
		activityHeartbeatTimeout: defaultActivityHeartbeat,
		activityRetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        defaultRetryInitialInterval,
			BackoffCoefficient:     2,
			MaximumInterval:        defaultRetryMaximumInterval,
			MaximumAttempts:        defaultRetryMaximumAttempts,
			NonRetryableErrorTypes: nonRetryableErrorTypes(),
		},
		workerOptions: worker.Options{
			DisableRegistrationAliasing: true,
		},
		workflowIDPrefix: defaultWorkflowIDPrefix,
	}
}

func defaultTaskQueueForInstance(instanceID string) (string, bool) {
	if instanceID == "" {
		return "", false
	}
	return instanceID, true
}

// WithTaskQueueForInstance sets how registry instance IDs are translated to
// Temporal task queues. The default uses the instance ID directly as the queue
// name.
func WithTaskQueueForInstance(fn TaskQueueForInstance) Option {
	return func(c *config) {
		if fn != nil {
			c.taskQueueForInstance = fn
		}
	}
}

// WithWorkflowTimeouts overrides workflow execution, run, and task timeouts
// used when CallRemote starts the routing workflow. Pass zero for a field to
// keep the default for that field.
func WithWorkflowTimeouts(execution, run, task time.Duration) Option {
	return func(c *config) {
		if execution > 0 {
			c.workflowExecutionTimeout = execution
		}
		if run > 0 {
			c.workflowRunTimeout = run
		}
		if task > 0 {
			c.workflowTaskTimeout = task
		}
	}
}

// WithActivityTimeouts overrides the activity schedule-to-close,
// start-to-close, and heartbeat timeouts. Pass zero for a field to keep the
// default for that field.
func WithActivityTimeouts(scheduleToClose, startToClose, heartbeat time.Duration) Option {
	return func(c *config) {
		if scheduleToClose > 0 {
			c.activityScheduleToClose = scheduleToClose
		}
		if startToClose > 0 {
			c.activityStartToClose = startToClose
		}
		if heartbeat > 0 {
			c.activityHeartbeatTimeout = heartbeat
		}
	}
}

// WithActivityRetryPolicy replaces the retry policy for the delivery activity.
// A nil policy keeps Temporal's server-side default retry behavior.
func WithActivityRetryPolicy(policy *temporal.RetryPolicy) Option {
	return func(c *config) {
		c.activityRetryPolicy = policy
	}
}

// WithWorkerOptions replaces the worker options used by ServeRemote.
func WithWorkerOptions(options worker.Options) Option {
	return func(c *config) {
		c.workerOptions = options
	}
}

// WithWorkflowIDPrefix sets the prefix used for generated routing workflow IDs.
func WithWorkflowIDPrefix(prefix string) Option {
	return func(c *config) {
		if prefix != "" {
			c.workflowIDPrefix = prefix
		}
	}
}

// WithCancelWorkflowOnContextError controls whether CallRemote asks Temporal to
// cancel the workflow when waiting for the result returns because ctx is done.
// The default is false, so Temporal may continue delivery until workflow/activity
// timeouts expire.
func WithCancelWorkflowOnContextError(cancel bool) Option {
	return func(c *config) {
		c.cancelWorkflowOnContextErr = cancel
	}
}
