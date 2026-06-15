package v16

import (
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16/profiles"
)

// RegisterSchemas registers all OCPP 1.6 schemas into r.
func RegisterSchemas(r *schema.Registry) error {
	return profiles.RegisterSchemas(r)
}
