package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

// TestLifecycleUnderRaceDetector is an ordinary life of a central system:
// charge points connect, exchange messages in both directions, some drop,
// the application reads Errors(), the server stops. Run with -race.
func TestLifecycleUnderRaceDetector(t *testing.T) {
	cs := h.StartCS(h.Opt{CallTimeout: time.Second, Before: func(cs *h.CS) {
		cs.SetChargePointDisconnectedHandler(func(cp chargePointConn) {})
	}})
	go func() {
		for range cs.Errors() {
		}
	}()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ { // few and paced: more concurrent senders trip the dispatcher deadlock of r01 and the test hangs
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("cp%02d", i)
			cp, err := h.Dial(cs.Port, id)
			if err != nil {
				return
			}
			go cp.AnswerCalls(`{"configurationKey":[]}`, 0)
			for j := 0; j < 10; j++ {
				_ = cp.Send(fmt.Sprintf(`[2,"h%d","Heartbeat",{}]`, j))
				_ = cs.SendRequestAsync(id, core.NewGetConfigurationRequest(nil), func(ocpp.Response, error) {})
				time.Sleep(10 * time.Millisecond)
			}
			if i%2 == 0 {
				cp.Conn.Close()
			}
		}()
	}
	wg.Wait()
	time.Sleep(1500 * time.Millisecond)
	cs.Stop()
	time.Sleep(300 * time.Millisecond)
}

type nopLogger struct{}

func (nopLogger) Debug(args ...interface{})                 {}
func (nopLogger) Debugf(format string, args ...interface{}) {}
func (nopLogger) Info(args ...interface{})                  {}
func (nopLogger) Infof(format string, args ...interface{})  {}
func (nopLogger) Error(args ...interface{})                 {}
func (nopLogger) Errorf(format string, args ...interface{}) {}

// TestSecondServerSetupWhileFirstServes: a second central system is set up
// (its module installs its logger, as every ocpp-go example does) while the
// first one is serving traffic. The logger is a package variable.
func TestSecondServerSetupWhileFirstServes(t *testing.T) {
	a := h.StartCS(h.Opt{})
	cp := h.MustDial(a.Port, "cp1")
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				_, _ = cp.Heartbeat(fmt.Sprintf("h%d", i), time.Second)
			}
		}
	}()
	time.Sleep(50 * time.Millisecond)
	ocppj.SetLogger(nopLogger{}) // setup of the second server
	ws.SetLogger(nopLogger{})
	b := h.StartCS(h.Opt{})
	_ = b
	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-done
}
