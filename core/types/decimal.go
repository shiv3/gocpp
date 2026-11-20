// Package types holds small value types shared by the generated message
// packages.
package types

import (
	"bytes"
	"fmt"

	"github.com/shopspring/decimal"
)

// Decimal is the Go type of an OCPP JSON Schema "number" field.
//
// It wraps shopspring's decimal.Decimal, so all arithmetic and formatting
// methods are available through the embedded value, but it always marshals to
// an unquoted JSON number. It does not depend on the process-global
// decimal.MarshalJSONWithoutQuotes, which the application owns.
//
// UnmarshalJSON accepts a JSON number and, leniently, a quoted numeric string.
type Decimal struct {
	decimal.Decimal
}

// New wraps a shopspring decimal.
func New(d decimal.Decimal) Decimal { return Decimal{Decimal: d} }

// NewFromInt returns a Decimal for an integer value.
func NewFromInt(v int64) Decimal { return Decimal{decimal.NewFromInt(v)} }

// NewFromFloat returns a Decimal for the shortest representation of v.
func NewFromFloat(v float64) Decimal { return Decimal{decimal.NewFromFloat(v)} }

// NewFromString parses a numeric string such as "32.5".
func NewFromString(s string) (Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return Decimal{}, err
	}
	return Decimal{d}, nil
}

// RequireFromString is like NewFromString but panics on error.
func RequireFromString(s string) Decimal { return Decimal{decimal.RequireFromString(s)} }

// Shopspring returns the underlying decimal.Decimal.
func (d Decimal) Shopspring() decimal.Decimal { return d.Decimal }

// MarshalJSON encodes the value as an unquoted JSON number.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(d.Decimal.String()), nil
}

// UnmarshalJSON decodes a JSON number or a quoted numeric string.
func (d *Decimal) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if string(b) == "null" {
		return nil
	}
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	v, err := decimal.NewFromString(string(b))
	if err != nil {
		return fmt.Errorf("types: invalid decimal %q: %w", b, err)
	}
	d.Decimal = v
	return nil
}

// MarshalText keeps the text form consistent with MarshalJSON.
func (d Decimal) MarshalText() ([]byte, error) { return []byte(d.Decimal.String()), nil }

// UnmarshalText parses a numeric string.
func (d *Decimal) UnmarshalText(b []byte) error {
	v, err := decimal.NewFromString(string(b))
	if err != nil {
		return err
	}
	d.Decimal = v
	return nil
}
