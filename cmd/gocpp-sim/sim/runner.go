package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shiv3/gocpp/cp"
)

// Result is the outcome of one step.
type Result struct {
	Action   string
	Response []byte
	Err      error
}

// Run connects a charge point and executes the scenario steps in order.
func Run(ctx context.Context, sc Scenario) ([]Result, error) {
	url := sc.CSMSURL + sc.CPID
	subproto := "ocpp" + sc.Version
	client := cp.NewClient(sc.CPID, url, cp.WithSubProtocols(subproto))
	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer client.Close()

	var results []Result
	for _, step := range sc.Steps {
		if step.DelayMs > 0 {
			select {
			case <-time.After(time.Duration(step.DelayMs) * time.Millisecond):
			case <-ctx.Done():
				return results, ctx.Err()
			}
		}
		payload, err := json.Marshal(step.Payload)
		if err != nil {
			results = append(results, Result{Action: step.Action, Err: err})
			continue
		}
		resp, err := cp.CallRaw(ctx, client, step.Action, payload)
		results = append(results, Result{Action: step.Action, Response: resp, Err: err})
	}
	return results, nil
}
