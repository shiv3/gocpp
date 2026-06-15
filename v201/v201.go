package v201

import (
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v201/profiles"
)

// RegisterSchemas registers all OCPP 2.0.1 schemas into r.
func RegisterSchemas(r *schema.Registry) error {
	return profiles.RegisterSchemas(r)
}
