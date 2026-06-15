// Package schemas embeds the OCPP 1.6 JSON schemas for runtime validation.
package schemas

import "embed"

// FS holds the embedded JSON schema files.
//
//go:embed *.json
var FS embed.FS
