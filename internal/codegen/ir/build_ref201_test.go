package ir

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildStructTree_ResolvesDefinitionsRef(t *testing.T) {
	schema := map[string]any{
		"title": "AuthorizeRequest",
		"type":  "object",
		"definitions": map[string]any{
			"IdTokenType": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"idToken": map[string]any{"type": "string", "maxLength": float64(36)},
					"type":    map[string]any{"type": "string", "enum": []any{"Central", "ISO14443"}},
				},
				"required": []any{"idToken", "type"},
			},
		},
		"properties": map[string]any{
			"idToken": map[string]any{"$ref": "#/definitions/IdTokenType"},
		},
		"required": []any{"idToken"},
	}
	structs, _, err := BuildStructTree("AuthorizeRequest", schema)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, s := range structs {
		names[s.GoName] = true
	}
	require.True(t, names["AuthorizeRequest"])
	require.True(t, names["IdTokenType"], "referenced definition must be generated as a struct")
}
