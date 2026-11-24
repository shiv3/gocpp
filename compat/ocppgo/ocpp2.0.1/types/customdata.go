// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package types

import (
	"bytes"
	"encoding/json"
	"errors"
)

// CustomData is the schema's CustomDataType: every OCPP 2.0.1 message and nested
// object may carry a "customData" object with a mandatory "vendorId" and, unlike
// any other object, arbitrary further properties. Those are kept in Extra
// (property name to raw JSON value) so that they survive a decode and re-encode.
type CustomData struct {
	VendorID string                     `json:"vendorId" validate:"required,max=255"`
	Extra    map[string]json.RawMessage `json:"-"`
}

// MarshalJSON writes vendorId and the Extra properties (an Extra entry named
// "vendorId" is ignored).
func (c CustomData) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, len(c.Extra)+1)
	for k, v := range c.Extra {
		m[k] = v
	}
	id, err := json.Marshal(c.VendorID)
	if err != nil {
		return nil, err
	}
	m["vendorId"] = id
	return json.Marshal(m)
}

// UnmarshalJSON reads vendorId and keeps every other property in Extra.
func (c *CustomData) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m == nil {
		return errors.New("customData must be an object")
	}
	c.VendorID, c.Extra = "", nil
	if raw, ok := m["vendorId"]; ok {
		if err := json.Unmarshal(bytes.TrimSpace(raw), &c.VendorID); err != nil {
			return err
		}
		delete(m, "vendorId")
	}
	if len(m) > 0 {
		c.Extra = m
	}
	return nil
}
