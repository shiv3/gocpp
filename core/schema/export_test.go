package schema

// Test-only exports for the external differential tests.

// RefValidateLenient is the pre-rewrite lenient path (lenient_ref_test.go).
func RefValidateLenient(v *Validator, raw []byte, opts LenientOptions) ([]byte, []string, error) {
	return refValidateLenient(v, raw, opts)
}

// FastDecode exposes fastDecode without schema-derived interning.
func FastDecode(raw []byte) (any, bool) { return fastDecode(raw, nil) }
