package routertemporal

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func TestRouteCallWorkflowRunsDeliveryActivity(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetDisableRegistrationAliasing(true)
	env := suite.NewTestWorkflowEnvironment()

	var gotCPID, gotAction string
	var gotRequest []byte
	acts := &remoteActivities{
		handler: func(_ context.Context, cpID, action string, req []byte) ([]byte, error) {
			gotCPID = cpID
			gotAction = action
			gotRequest = cloneBytes(req)
			return []byte(`{"status":"Accepted"}`), nil
		},
	}

	env.RegisterWorkflowWithOptions(routeCallWorkflow, workflow.RegisterOptions{Name: routeCallWorkflowName})
	env.RegisterActivityWithOptions(acts.deliver, activity.RegisterOptions{Name: routeCallActivityName})

	env.ExecuteWorkflow(routeCallWorkflowName, routeCallWorkflowInput{
		CPID:                    "CP_1",
		Action:                  "Reset",
		Request:                 []byte(`{"type":"Immediate"}`),
		TargetTaskQueue:         "instance-b",
		ActivityScheduleToClose: newTimeDuration(time.Minute),
		ActivityStartToClose:    newTimeDuration(10 * time.Second),
		ActivityRetryPolicy: &temporal.RetryPolicy{
			InitialInterval:        time.Millisecond,
			BackoffCoefficient:     1,
			MaximumInterval:        time.Millisecond,
			MaximumAttempts:        1,
			NonRetryableErrorTypes: nonRetryableErrorTypes(),
		},
	})

	if !env.IsWorkflowCompleted() {
		t.Fatal("workflow did not complete")
	}
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow failed: %v", err)
	}

	var out routeCallActivityOutput
	if err := env.GetWorkflowResult(&out); err != nil {
		t.Fatalf("get workflow result: %v", err)
	}
	if string(out.Response) != `{"status":"Accepted"}` {
		t.Fatalf("response = %s", out.Response)
	}
	if gotCPID != "CP_1" || gotAction != "Reset" || string(gotRequest) != `{"type":"Immediate"}` {
		t.Fatalf("handler got cpID=%q action=%q req=%s", gotCPID, gotAction, gotRequest)
	}
}

func TestDeliverActivityInvokesHandler(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()

	acts := &remoteActivities{
		handler: func(_ context.Context, cpID, action string, req []byte) ([]byte, error) {
			if cpID != "CP_1" || action != "TriggerMessage" || string(req) != `{"requestedMessage":"StatusNotification"}` {
				t.Fatalf("handler got cpID=%q action=%q req=%s", cpID, action, req)
			}
			return []byte(`{"status":"Accepted"}`), nil
		},
	}
	env.RegisterActivityWithOptions(acts.deliver, activity.RegisterOptions{Name: routeCallActivityName})

	value, err := env.ExecuteActivity(routeCallActivityName, routeCallActivityInput{
		CPID:    "CP_1",
		Action:  "TriggerMessage",
		Request: []byte(`{"requestedMessage":"StatusNotification"}`),
	})
	if err != nil {
		t.Fatalf("activity failed: %v", err)
	}

	var out routeCallActivityOutput
	if err := value.Get(&out); err != nil {
		t.Fatalf("decode activity result: %v", err)
	}
	if string(out.Response) != `{"status":"Accepted"}` {
		t.Fatalf("response = %s", out.Response)
	}
}

func TestWorkflowMapsHandlerNotLocalToTemporalApplicationError(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	suite.SetDisableRegistrationAliasing(true)
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(routeCallWorkflow, workflow.RegisterOptions{Name: routeCallWorkflowName})
	env.RegisterActivityWithOptions((&remoteActivities{
		handler: func(context.Context, string, string, []byte) ([]byte, error) {
			return nil, storage.ErrNotLocal
		},
	}).deliver, activity.RegisterOptions{Name: routeCallActivityName})

	env.ExecuteWorkflow(routeCallWorkflowName, routeCallWorkflowInput{
		CPID:                    "CP_1",
		Action:                  "Reset",
		TargetTaskQueue:         "instance-b",
		ActivityScheduleToClose: newTimeDuration(time.Minute),
		ActivityStartToClose:    newTimeDuration(10 * time.Second),
		ActivityRetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:        1,
			NonRetryableErrorTypes: nonRetryableErrorTypes(),
		},
	})

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected workflow error")
	}
	var appErr *temporal.ApplicationError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected application error, got %T: %v", err, err)
	}
	if appErr.Type() != temporalErrorTypeNotLocal {
		t.Fatalf("application error type = %q", appErr.Type())
	}
}

func TestMapTemporalErrorPreservesStorageSentinels(t *testing.T) {
	err := temporal.NewNonRetryableApplicationError(
		storage.ErrRouterNotImplemented.Error(),
		temporalErrorTypeRouterNotImplemented,
		nil,
	)
	if got := mapTemporalError(err); got != storage.ErrRouterNotImplemented {
		t.Fatalf("mapped error = %v", got)
	}
}
