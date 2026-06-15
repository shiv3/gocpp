package routertemporal

import (
	"context"
	"errors"

	"github.com/shiv3/gocpp/core/storage"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	routeCallWorkflowName = "gocpp.router_temporal.RouteCall"
	routeCallActivityName = "gocpp.router_temporal.DeliverCall"

	temporalErrorTypeNotLocal             = "gocpp.storage.ErrNotLocal"
	temporalErrorTypeRouterNotImplemented = "gocpp.storage.ErrRouterNotImplemented"
)

type routeCallWorkflowInput struct {
	CPID                     string
	Action                   string
	Request                  []byte
	TargetTaskQueue          string
	ActivityScheduleToClose  timeDuration
	ActivityStartToClose     timeDuration
	ActivityHeartbeatTimeout timeDuration
	ActivityRetryPolicy      *temporal.RetryPolicy
}

type routeCallActivityInput struct {
	CPID    string
	Action  string
	Request []byte
}

type routeCallActivityOutput struct {
	Response []byte
}

type remoteActivities struct {
	handler storage.RemoteHandler
}

func routeCallWorkflow(ctx workflow.Context, input routeCallWorkflowInput) (routeCallActivityOutput, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		TaskQueue:              input.TargetTaskQueue,
		ScheduleToCloseTimeout: input.ActivityScheduleToClose.Duration(),
		StartToCloseTimeout:    input.ActivityStartToClose.Duration(),
		HeartbeatTimeout:       input.ActivityHeartbeatTimeout.Duration(),
		RetryPolicy:            input.ActivityRetryPolicy,
		DisableEagerExecution:  true,
		Summary:                input.CPID + " " + input.Action,
	})

	var out routeCallActivityOutput
	err := workflow.ExecuteActivity(ctx, routeCallActivityName, routeCallActivityInput{
		CPID:    input.CPID,
		Action:  input.Action,
		Request: cloneBytes(input.Request),
	}).Get(ctx, &out)
	if err != nil {
		return routeCallActivityOutput{}, err
	}
	out.Response = cloneBytes(out.Response)
	return out, nil
}

func (a *remoteActivities) deliver(ctx context.Context, input routeCallActivityInput) (routeCallActivityOutput, error) {
	if a == nil || a.handler == nil {
		return routeCallActivityOutput{}, temporal.NewNonRetryableApplicationError(
			storage.ErrRouterNotImplemented.Error(),
			temporalErrorTypeRouterNotImplemented,
			nil,
		)
	}

	resp, err := a.handler(ctx, input.CPID, input.Action, cloneBytes(input.Request))
	if err != nil {
		return routeCallActivityOutput{}, activityError(err)
	}
	return routeCallActivityOutput{Response: cloneBytes(resp)}, nil
}

func activityError(err error) error {
	switch {
	case errors.Is(err, storage.ErrNotLocal):
		return temporal.NewNonRetryableApplicationError(
			storage.ErrNotLocal.Error(),
			temporalErrorTypeNotLocal,
			nil,
		)
	case errors.Is(err, storage.ErrRouterNotImplemented):
		return temporal.NewNonRetryableApplicationError(
			storage.ErrRouterNotImplemented.Error(),
			temporalErrorTypeRouterNotImplemented,
			nil,
		)
	default:
		return err
	}
}

func workflowRegisterOptions() workflow.RegisterOptions {
	return workflow.RegisterOptions{Name: routeCallWorkflowName}
}

func activityRegisterOptions() activity.RegisterOptions {
	return activity.RegisterOptions{Name: routeCallActivityName}
}

func nonRetryableErrorTypes() []string {
	return []string{
		temporalErrorTypeNotLocal,
		temporalErrorTypeRouterNotImplemented,
	}
}
