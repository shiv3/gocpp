// Command cs-ocppgo is the ocpp-go half of the parity test CSMS pair.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/parity/common"
)

func main() {
	listen := flag.String("listen", "", "listen address, e.g. 127.0.0.1:9000 (required; only the port is used, ocpp-go binds all interfaces)")
	eventsPath := flag.String("events", "", "append events as JSONL to this file")
	callTimeout := flag.Duration("call-timeout", 10*time.Second, "timeout for CSMS-initiated calls")
	version := flag.String("version", "1.6", "OCPP version: 1.6 or 2.0.1 (ocpp-go has no 2.1)")
	flag.Parse()
	if *listen == "" {
		log.Fatal("-listen is required")
	}
	_, portStr, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal(err)
	}

	rec, err := common.NewRecorder(*eventsPath)
	if err != nil {
		log.Fatal(err)
	}
	lg := &captureLogger{rec: rec}
	ws.SetLogger(lg)
	ocppj.SetLogger(lg)

	// Library defaults everywhere except the dispatcher timeout, which is set
	// to -call-timeout (the default is 30s) so a call that gets no answer is
	// cancelled and unblocks the per-CP request queue.
	dispatcher := ocppj.NewDefaultServerDispatcher(ocppj.NewFIFOQueueMap(0))
	dispatcher.SetTimeout(*callTimeout)
	wsServer := ws.NewServer()

	be := &backend{ws: wsServer, rec: rec, lg: lg}
	hk := hooks{
		// Called before the upgrade: an id that is still registered is about to
		// be replaced (ocpp-go is last-wins).
		validate: func(id string, r *http.Request) bool {
			if rec.IsConnected(id) {
				lg.markReplacing(id)
			}
			return true
		},
		connect: func(id string) { rec.Connect(id, "") },
		disconnect: func(id string) {
			cause, raw := be.cause(id)
			rec.Disconnect(id, cause, raw)
		},
	}
	var api csAPI
	switch *version {
	case "1.6":
		api, be.requests = setup16(rec, wsServer, dispatcher, hk)
	case "2.0.1":
		api, be.requests = setup201(rec, wsServer, dispatcher, hk)
	default:
		log.Fatalf("ocpp-go does not implement OCPP %s (want 1.6 or 2.0.1)", *version)
	}
	be.api = api

	// ocpp-go also reports central system errors through this channel. The
	// ws.Server errors channel is not drained: the same errors arrive through
	// the ws logger, so reading both would record each of them twice.
	go func() {
		for err := range api.Errors() {
			rec.Error("", err.Error())
		}
	}()

	ctl := common.Control(rec, be, *callTimeout)
	wsServer.AddHttpHandler("/control/{rest:.*}", ctl.ServeHTTP)

	fmt.Fprintf(os.Stderr, "cs-ocppgo listening on :%d (requested %s, version %s)\n", port, *listen, *version)
	api.Start(port, "/ocpp/{ws-id}")
}

// csAPI is the part of ocpp16.CentralSystem and ocpp2.CSMS the harness uses.
type csAPI interface {
	SendRequestAsync(clientId string, request ocpp.Request, callback func(ocpp.Response, error)) error
	Errors() <-chan error
	Start(listenPort int, listenPath string)
}

// hooks are the connection callbacks, wired per version.
type hooks struct {
	validate   func(id string, r *http.Request) bool
	connect    func(id string)
	disconnect func(id string)
}

// ---------------------------------------------------------------- control

type backend struct {
	api      csAPI
	requests map[string]func() ocpp.Request
	ws       *ws.Server
	rec      *common.Recorder
	lg       *captureLogger

	mu         sync.Mutex
	serverStop map[string]bool // ids whose close we initiated
}

func (b *backend) Lib() string { return "ocpp-go" }

func (b *backend) Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/lorenzodonini/ocpp-go" {
				if d.Replace != nil {
					return d.Replace.Path + "@" + d.Replace.Version
				}
				return d.Version
			}
		}
	}
	return "unknown"
}

func (b *backend) Call(ctx context.Context, cpID, action string, payload json.RawMessage) (json.RawMessage, error) {
	mk, ok := b.requests[action]
	if !ok {
		return nil, &common.KindedError{Kind: common.KindInvalid, Err: fmt.Errorf("unsupported action %q", action)}
	}
	req := mk()
	if err := json.Unmarshal(payload, req); err != nil {
		return nil, &common.KindedError{Kind: common.KindInvalid, Err: err}
	}
	type result struct {
		resp ocpp.Response
		err  error
	}
	done := make(chan result, 1)
	err := b.api.SendRequestAsync(cpID, req, func(resp ocpp.Response, err error) {
		done <- result{resp, err}
	})
	if err != nil {
		kind := common.KindOther
		switch {
		case strings.Contains(fmt.Sprintf("%T", err), "ValidationErrors"):
			kind = common.KindInvalid
		case strings.Contains(err.Error(), "not connected"), strings.Contains(err.Error(), "no client"):
			kind = common.KindNotConnected
		}
		return nil, &common.KindedError{Kind: kind, Err: err}
	}
	select {
	case <-ctx.Done():
		return nil, &common.KindedError{Kind: common.KindTimeout, Err: ctx.Err()}
	case r := <-done:
		if r.err != nil {
			var oe *ocpp.Error
			if errors.As(r.err, &oe) {
				if oe.Code == ocppj.GenericError && oe.Description == "Request timed out" {
					return nil, &common.KindedError{Kind: common.KindTimeout, Err: errors.New(oe.Description)}
				}
				return nil, &common.CallError{Code: string(oe.Code), Description: oe.Description}
			}
			return nil, r.err
		}
		return json.Marshal(r.resp)
	}
}

// Raw is unsupported: ocpp-go's public API only writes typed, validated
// messages (ws.Server.Write is on the unexported-by-contract ocppj path and
// would bypass request/response bookkeeping), so no raw frame injection.
func (b *backend) Raw(string, string) error { return common.ErrUnsupported }

func (b *backend) stop(id string) error {
	b.mu.Lock()
	if b.serverStop == nil {
		b.serverStop = map[string]bool{}
	}
	b.serverStop[id] = true
	b.mu.Unlock()
	err := b.ws.StopConnection(id, websocket.CloseError{Code: websocket.CloseNormalClosure})
	if err != nil {
		b.mu.Lock()
		delete(b.serverStop, id)
		b.mu.Unlock()
	}
	return err
}

// Disconnect uses ws.Server.StopConnection with a normal closure (1000).
func (b *backend) Disconnect(id string) error { return b.stop(id) }

// Shutdown calls StopConnection for every connected id. ws.Server.Stop would
// also close the HTTP listener (and with it this control API), so it is not
// used.
func (b *backend) Shutdown() error {
	var errs []string
	for _, id := range b.rec.Connections() {
		if err := b.stop(id); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// cause decides the normalized disconnect cause. ocpp-go does not hand the
// application a cause, so it is inferred from what we initiated and from the
// library's own debug log of the read error.
func (b *backend) cause(id string) (cause, raw string) {
	b.mu.Lock()
	stopped := b.serverStop[id]
	delete(b.serverStop, id)
	b.mu.Unlock()
	readErr := b.lg.takeReadError(id)
	switch {
	case stopped:
		return common.CauseServerClosed, "StopConnection"
	case b.lg.takeReplacing(id):
		return common.CauseReplaced, "client already exists, closing stale connection"
	case readErr != "":
		return common.ClassifyReadError(readErr), readErr
	}
	return common.CauseUnknown, ""
}

// ---------------------------------------------------------------- logging

var readErrRe = regexp.MustCompile(`^handling read error for (.+?): (.*)$`)

// captureLogger implements ocpp-go's logging.Logger. Error-level lines become
// "error" events; one Debug line is parsed to learn why a read loop ended.
type captureLogger struct {
	rec       *common.Recorder
	mu        sync.Mutex
	readErrs  map[string]string
	replacing map[string]bool
}

func (l *captureLogger) Debug(args ...interface{}) {}
func (l *captureLogger) Info(args ...interface{})  {}
func (l *captureLogger) Infof(format string, args ...interface{}) {
}
func (l *captureLogger) Debugf(format string, args ...interface{}) {
	if !strings.HasPrefix(format, "handling read error") {
		return
	}
	m := readErrRe.FindStringSubmatch(fmt.Sprintf(format, args...))
	if m == nil {
		return
	}
	l.mu.Lock()
	if l.readErrs == nil {
		l.readErrs = map[string]string{}
	}
	l.readErrs[m[1]] = m[2]
	l.mu.Unlock()
}
func (l *captureLogger) Error(args ...interface{}) {
	l.rec.Add(common.Event{Kind: common.KindError, Level: "error", Detail: strings.TrimSpace(fmt.Sprint(args...)), Msg: "library log"})
}
func (l *captureLogger) Errorf(format string, args ...interface{}) {
	l.rec.Add(common.Event{Kind: common.KindError, Level: "error", Detail: fmt.Sprintf(format, args...), Msg: "library log"})
}

func (l *captureLogger) takeReadError(id string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.readErrs[id]
	delete(l.readErrs, id)
	return v
}

func (l *captureLogger) markReplacing(id string) {
	l.mu.Lock()
	if l.replacing == nil {
		l.replacing = map[string]bool{}
	}
	l.replacing[id] = true
	l.mu.Unlock()
}

func (l *captureLogger) takeReplacing(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.replacing[id]
	delete(l.replacing, id)
	return v
}
