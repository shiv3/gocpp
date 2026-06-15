package storage

import "context"

// ConfigStore persists charge point configuration key/value pairs. The library does
// not interpret values; it only stores what ChangeConfiguration/SetVariables produce.
type ConfigStore interface {
	Put(ctx context.Context, cpID, key, value string) error
	Get(ctx context.Context, cpID, key string) (string, bool, error)
	List(ctx context.Context, cpID string) (map[string]string, error)
	Delete(ctx context.Context, cpID, key string) error
}
