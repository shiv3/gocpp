// Package actions knows which OCPP actions exist per version and in which
// direction, and holds the schema registry of all three versions.
//
// The action sets are read from gocpp's generated handler interfaces (a
// CPHandler method per CSMS-initiated action, a CSMSHandler method per
// charge-point-initiated one), so they follow the generated code.
package actions

import (
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	"github.com/shiv3/gocpp/v201"
	v201h "github.com/shiv3/gocpp/v201/handlers"
	"github.com/shiv3/gocpp/v21"
	v21h "github.com/shiv3/gocpp/v21/handlers"
)

// Versions are the supported OCPP versions.
var Versions = []ocppj.Version{ocppj.V16, ocppj.V201, ocppj.V21}

// Set is the action set of one version and direction.
type Set struct {
	// Calls are actions answered with a CALLRESULT.
	Calls []string
	// Sends are unconfirmed OCPP 2.1 SEND actions.
	Sends []string
}

// Has reports whether action is a CALL of the set.
func (s Set) Has(action string) bool {
	_, ok := slices.BinarySearch(s.Calls, action)
	return ok
}

// HasSend reports whether action is a SEND of the set.
func (s Set) HasSend(action string) bool {
	_, ok := slices.BinarySearch(s.Sends, action)
	return ok
}

type table struct {
	byCSMS map[ocppj.Version]Set
	byCP   map[ocppj.Version]Set
}

var load = sync.OnceValue(func() table {
	return table{
		byCSMS: map[ocppj.Version]Set{
			ocppj.V16:  fromInterface(reflect.TypeFor[v16h.CPHandler]()),
			ocppj.V201: fromInterface(reflect.TypeFor[v201h.CPHandler]()),
			ocppj.V21:  fromInterface(reflect.TypeFor[v21h.CPHandler]()),
		},
		byCP: map[ocppj.Version]Set{
			ocppj.V16:  fromInterface(reflect.TypeFor[v16h.CSMSHandler]()),
			ocppj.V201: fromInterface(reflect.TypeFor[v201h.CSMSHandler]()),
			ocppj.V21:  fromInterface(reflect.TypeFor[v21h.CSMSHandler]()),
		},
	}
})

// fromInterface lists the On<Action> methods of a generated handler
// interface; a method returning only an error handles a SEND.
func fromInterface(t reflect.Type) Set {
	var s Set
	for i := range t.NumMethod() {
		m := t.Method(i)
		action, ok := strings.CutPrefix(m.Name, "On")
		if !ok {
			continue
		}
		if m.Type.NumOut() == 1 {
			s.Sends = append(s.Sends, action)
		} else {
			s.Calls = append(s.Calls, action)
		}
	}
	slices.Sort(s.Calls)
	slices.Sort(s.Sends)
	return s
}

// SentByCSMS returns the actions the CSMS may send to a charge point of v.
func SentByCSMS(v ocppj.Version) Set { return load().byCSMS[v] }

// SentByCP returns the actions a charge point of v may send to the CSMS.
func SentByCP(v ocppj.Version) Set { return load().byCP[v] }

// Schemas returns the schema registry of all versions (built once).
var Schemas = sync.OnceValues(func() (*schema.Registry, error) {
	reg := schema.NewRegistry()
	for _, register := range []func(*schema.Registry) error{v16.RegisterSchemas, v201.RegisterSchemas, v21.RegisterSchemas} {
		if err := register(reg); err != nil {
			return nil, err
		}
	}
	return reg, nil
})
