// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

// Package synclog is the package logger of ocppj and ws: ocpp-go keeps it in a
// plain package variable that SetLogger writes while connections read it, which
// the race detector reports. Here the target is swapped atomically.
package synclog

import (
	"sync/atomic"

	"github.com/lorenzodonini/ocpp-go/logging"
)

// Logger implements logging.Logger by forwarding to the logger last passed to Set.
type Logger struct {
	p atomic.Pointer[logging.Logger]
}

// New returns a Logger that discards everything until Set is called.
func New() *Logger {
	l := &Logger{}
	l.Set(&logging.VoidLogger{})
	return l
}

// Set replaces the target. It is safe to call at any time. A nil argument panics,
// as ocpp-go's SetLogger does.
func (l *Logger) Set(target logging.Logger) {
	if target == nil {
		panic("cannot set a nil logger")
	}
	l.p.Store(&target)
}

func (l *Logger) t() logging.Logger { return *l.p.Load() }

func (l *Logger) Debug(args ...interface{})                 { l.t().Debug(args...) }
func (l *Logger) Debugf(format string, args ...interface{}) { l.t().Debugf(format, args...) }
func (l *Logger) Info(args ...interface{})                  { l.t().Info(args...) }
func (l *Logger) Infof(format string, args ...interface{})  { l.t().Infof(format, args...) }
func (l *Logger) Error(args ...interface{})                 { l.t().Error(args...) }
func (l *Logger) Errorf(format string, args ...interface{}) { l.t().Errorf(format, args...) }
