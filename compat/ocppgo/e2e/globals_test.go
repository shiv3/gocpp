// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
	"github.com/sirupsen/logrus"
)

// TestPackageSettersAreRaceFree: the package level setters of ocppj and ws may be
// called while connections are exchanging messages (the race detector fails the
// test if they are not synchronised). Run under -race.
func TestPackageSettersAreRaceFree(t *testing.T) {
	c := startCentral(t)
	cp := connectCP(t, c, "CP-SET", &cpHandler{})

	var n atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	setters := []func(){
		func() { l := logrus.New(); l.SetLevel(logrus.PanicLevel); ocppj.SetLogger(l) },
		func() { l := logrus.New(); l.SetLevel(logrus.PanicLevel); ws.SetLogger(l) },
		func() { ocppj.SetMessageValidation(true) },
		func() { ocppj.SetHTMLEscape(true) },
		func() { ocppj.SetMessageIdGenerator(func() string { return fmt.Sprint(n.Add(1)) }) },
	}
	for _, f := range setters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					f()
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}
	for i := 0; i < 50; i++ {
		if _, err := cp.Heartbeat(); err != nil {
			t.Fatal(err)
		}
		if err := c.cs.SendRequestAsync("CP-SET", core.NewResetRequest(core.ResetTypeSoft), func(ocpp.Response, error) {}); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
}

// TestNilLoggerPanics: a nil logger is refused with the documented panic, nothing else
// changes.
func TestNilLoggerPanics(t *testing.T) {
	for name, f := range map[string]func(){
		"ocppj": func() { ocppj.SetLogger(nil) },
		"ws":    func() { ws.SetLogger(nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s.SetLogger(nil) did not panic", name)
				}
			}()
			f()
		}()
	}
}
