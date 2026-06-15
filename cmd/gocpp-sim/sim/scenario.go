// Package sim implements a charge point simulator driven by YAML scenarios.
package sim

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// Step is one simulator action.
type Step struct {
	Action  string         `yaml:"action"`
	Payload map[string]any `yaml:"payload"`
	DelayMs int            `yaml:"delayMs"`
}

// Scenario is a full simulator run.
type Scenario struct {
	Version string `yaml:"version"`
	CPID    string `yaml:"cpId"`
	CSMSURL string `yaml:"csmsUrl"`
	Steps   []Step `yaml:"steps"`
}

// ParseScenario parses scenario YAML.
func ParseScenario(b []byte) (Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(b, &s); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario: %w", err)
	}
	if s.Version == "" || s.CPID == "" || s.CSMSURL == "" {
		return Scenario{}, fmt.Errorf("scenario missing version/cpId/csmsUrl")
	}
	return s, nil
}
