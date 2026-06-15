package loader

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadSchema reads a JSON Schema file into a generic map.
func LoadSchema(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", path, err)
	}
	return m, nil
}
