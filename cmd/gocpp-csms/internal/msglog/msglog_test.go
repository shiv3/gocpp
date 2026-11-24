package msglog

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/servertest"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
)

func frame(cpID string, dir observability.FrameDirection, raw string) observability.FrameEvent {
	return observability.FrameEvent{CPID: cpID, Version: ocppj.V16, Direction: dir, Raw: []byte(raw)}
}

func TestParseCorrelateRedact(t *testing.T) {
	ring := NewRing(10)
	l := New(ring.Add)
	l.Observe(frame("CP1", observability.FrameIn, `[2,"a1","Heartbeat",{}]`))
	l.Observe(frame("CP1", observability.FrameOut, `[3,"a1",{"currentTime":"2026-01-01T00:00:00Z"}]`))
	l.Observe(frame("CP1", observability.FrameOut, `[2,"b1","ChangeConfiguration",{"key":"AuthorizationKey","value":"topsecret"}]`))
	l.Observe(frame("CP1", observability.FrameIn, `[4,"b1","NotSupported","nope",{}]`))
	l.Observe(frame("CP2", observability.FrameIn, `not json`))
	l.Observe(frame("CP2", observability.FrameOut, `[2,"c1","SetVariables",{"setVariableData":[{"component":{"name":"SecurityCtrlr"},"variable":{"name":"BasicAuthPassword"},"attributeValue":"pw-pw-pw-pw-pw-pw"}]}]`))

	got := ring.Query(Filter{}, 0)
	if len(got) != 6 {
		t.Fatalf("%d entries", len(got))
	}
	for i, e := range got {
		if e.Seq != int64(i+1) {
			t.Fatalf("entry %d seq %d", i, e.Seq)
		}
	}
	if e := got[1]; e.Type != "CALLRESULT" || e.Action != "Heartbeat" || e.Direction != Out || string(e.Payload) != `{"currentTime":"2026-01-01T00:00:00Z"}` {
		t.Fatalf("result %+v", e)
	}
	if e := got[2]; strings.Contains(string(e.Payload), "topsecret") || !strings.Contains(string(e.Payload), Redacted) {
		t.Fatalf("not redacted: %s", e.Payload)
	}
	if e := got[3]; e.Type != "CALLERROR" || e.Action != "ChangeConfiguration" || e.ErrorCode != "NotSupported" || e.ErrorDescription != "nope" {
		t.Fatalf("error %+v", e)
	}
	if e := got[4]; e.Raw != "not json" || e.Type != "" {
		t.Fatalf("unparsable %+v", e)
	}
	if e := got[5]; strings.Contains(string(e.Payload), "pw-pw") {
		t.Fatalf("not redacted: %s", e.Payload)
	}

	if n := len(ring.Query(Filter{Action: "ChangeConfiguration"}, 0)); n != 2 {
		t.Fatalf("action filter: %d", n)
	}
	if n := len(ring.Query(Filter{CPID: "CP2", Direction: Out}, 0)); n != 1 {
		t.Fatalf("cp+direction filter: %d", n)
	}
	// Without since: the most recent; with since: the first ones after it.
	if q := ring.Query(Filter{}, 2); len(q) != 2 || q[0].Seq != 5 || q[1].Seq != 6 {
		t.Fatalf("latest: %+v", q)
	}
	if q := ring.Query(Filter{SinceSeq: 2, HasSinceSeq: true}, 2); len(q) != 2 || q[0].Seq != 3 || q[1].Seq != 4 {
		t.Fatalf("since: %+v", q)
	}

	// The ring keeps the last entries.
	small := NewRing(2)
	for i := range 5 {
		small.Add(Entry{Seq: int64(i + 1)})
	}
	if q := small.Query(Filter{}, 0); len(q) != 2 || q[0].Seq != 4 || q[1].Seq != 5 {
		t.Fatalf("wrap: %+v", q)
	}
}

func messages(t *testing.T, s *servertest.Server, query string) []Entry {
	t.Helper()
	status, body := s.Control(http.MethodGet, "/messages"+query, "")
	if status != 200 {
		t.Fatalf("GET /messages%s: %d %s", query, status, body)
	}
	var env struct {
		Result []Entry `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	return env.Result
}

func TestMessagesEndpoint(t *testing.T) {
	file := filepath.Join(t.TempDir(), "frames.jsonl")
	s := servertest.Start(t, []string{"--message-log", "--message-log-file", file}, NewExtension())
	c := s.MustDial("CP1", "/ocpp/CP1")
	servertest.Call(t, c, "BootNotification", `{"chargePointModel":"M","chargePointVendor":"V"}`)
	// A CSMS-initiated call the charge point does not implement: CALLERROR back.
	if status, body := s.Control(http.MethodPost, "/command", `{"cpId":"CP1","action":"ClearCache","payload":{}}`); status != 200 {
		t.Fatalf("command: %d %s", status, body)
	}
	servertest.WaitFor(t, "4 messages", func() bool { return len(messages(t, s, "?cp=CP1")) == 4 })

	got := messages(t, s, "?cp=CP1")
	want := []string{"in CALL BootNotification", "out CALLRESULT BootNotification", "out CALL ClearCache", "in CALLERROR ClearCache"}
	for i, e := range got {
		if s := e.Direction + " " + e.Type + " " + e.Action; s != want[i] {
			t.Fatalf("entry %d: %q, want %q (%+v)", i, s, want[i], got)
		}
	}
	if n := len(messages(t, s, "?action=ClearCache&direction=in")); n != 1 {
		t.Fatalf("action+direction: %d", n)
	}
	if q := messages(t, s, fmt.Sprintf("?since=%d&limit=1", got[1].Seq)); len(q) != 1 || q[0].Seq != got[2].Seq {
		t.Fatalf("since/limit: %+v", q)
	}
	if n := len(messages(t, s, "?since="+url.QueryEscape(got[2].Time.Format(time.RFC3339Nano)))); n < 2 {
		t.Fatalf("since time: %d", n)
	}
	if status, _ := s.Control(http.MethodGet, "/messages?direction=up", ""); status != 400 {
		t.Fatalf("bad direction: %d", status)
	}
	if status, _ := s.Control(http.MethodGet, "/messages?limit=0", ""); status != 400 {
		t.Fatalf("bad limit: %d", status)
	}

	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(b)), "\n"); len(lines) != 4 {
		t.Fatalf("file lines: %d\n%s", len(lines), b)
	}
	if status, body := s.Control(http.MethodDelete, "/messages", ""); status != 200 || body != `{"ok":true,"result":{"deleted":4}}` {
		t.Fatalf("clear: %d %s", status, body)
	}
}

func TestMessagesStream(t *testing.T) {
	s := servertest.Start(t, []string{"--message-log"}, NewExtension())
	c := s.MustDial("CP1", "/ocpp/CP1")
	servertest.Call(t, c, "Heartbeat", `{}`) // seq 1, 2

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.ControlAddr().String()+"/messages/stream?cp=CP1&direction=in", nil)
	req.Header.Set("Last-Event-ID", "0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	events := make(chan Entry, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var e Entry
				if json.Unmarshal([]byte(data), &e) == nil {
					events <- e
				}
			}
		}
		close(events)
	}()
	next := func() Entry {
		t.Helper()
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("stream ended")
			}
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
		}
		return Entry{}
	}
	// The backlog (Last-Event-ID 0 = from the start), then live entries.
	if e := next(); e.Action != "Heartbeat" || e.Type != "CALL" {
		t.Fatalf("backlog %+v", e)
	}
	servertest.Call(t, c, "StatusNotification", `{"connectorId":1,"errorCode":"NoError","status":"Available"}`)
	if e := next(); e.Action != "StatusNotification" || e.Direction != In {
		t.Fatalf("live %+v", e)
	}
}

// TestWireOrder: with concurrent charge points and no inbound serialisation,
// each connection's entries are in its wire order (every CALL before its
// answer, the charge point's CALLs in send order) and seq is gapless; the
// --record-frames stdout lines carry the same frames.
func TestWireOrder(t *testing.T) {
	s := servertest.Start(t, []string{"--message-log", "--record", "--record-frames"}, NewExtension())
	const cps, calls = 4, 25
	var wg sync.WaitGroup
	for i := range cps {
		c := s.MustDial(fmt.Sprintf("CP%d", i), fmt.Sprintf("/ocpp/CP%d", i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range calls {
				servertest.Call(t, c, "DataTransfer", fmt.Sprintf(`{"vendorId":"v","messageId":"%d"}`, j))
			}
		}()
	}
	wg.Wait()
	total := cps * calls * 2
	servertest.WaitFor(t, "all frames", func() bool { return len(messages(t, s, fmt.Sprintf("?limit=%d", total+10))) == total })
	all := messages(t, s, fmt.Sprintf("?limit=%d", total))
	next := map[string]int{}
	open := map[string]string{}
	for i, e := range all {
		if e.Seq != int64(i+1) {
			t.Fatalf("seq gap at %d: %d", i, e.Seq)
		}
		switch e.Type {
		case "CALL":
			var p struct {
				MessageID string `json:"messageId"`
			}
			_ = json.Unmarshal(e.Payload, &p)
			if p.MessageID != fmt.Sprint(next[e.CPID]) {
				t.Fatalf("%s: CALL %s out of order, want %d", e.CPID, p.MessageID, next[e.CPID])
			}
			next[e.CPID]++
			open[e.CPID+e.MsgID] = e.Action
		case "CALLRESULT":
			if _, ok := open[e.CPID+e.MsgID]; !ok || e.Action != "DataTransfer" {
				t.Fatalf("answer before its CALL: %+v", e)
			}
			delete(open, e.CPID+e.MsgID)
		}
	}

	lines := strings.Split(strings.TrimSpace(s.Stdout.String()), "\n")
	if len(lines) != total {
		t.Fatalf("stdout lines %d, want %d", len(lines), total)
	}
	for i, l := range lines {
		var r struct {
			Seq       int64           `json:"seq"`
			CPID      string          `json:"cpId"`
			Action    string          `json:"action"`
			Payload   json.RawMessage `json:"payload"`
			Direction string          `json:"direction"`
			Type      string          `json:"type"`
		}
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		if r.Seq != int64(i+1) || r.CPID == "" || r.Action != "DataTransfer" || r.Direction == "" || r.Type == "" {
			t.Fatalf("line %d: %s", i, l)
		}
		if i == 0 && !strings.HasPrefix(l, `{"seq":1,"cpId":`) {
			t.Fatalf("line layout: %s", l)
		}
	}
}

func TestRecordFramesNeedsRecord(t *testing.T) {
	if _, _, err := servertest.New(t, []string{"--record-frames"}, NewExtension()); err == nil {
		t.Fatal("--record-frames without --record accepted")
	}
}

// TestOffByDefault: without its flags the extension adds no route and
// leaves --record lines as they are.
func TestOffByDefault(t *testing.T) {
	s := servertest.Start(t, []string{"--record"}, NewExtension())
	if status, _ := s.Control(http.MethodGet, "/messages", ""); status != http.StatusNotFound {
		t.Fatalf("/messages without --message-log: %d", status)
	}
	c := s.MustDial("CP1", "/ocpp/CP1")
	servertest.Call(t, c, "Heartbeat", `{}`)
	servertest.WaitFor(t, "record line", func() bool { return s.Stdout.String() != "" })
	if got := strings.TrimSpace(s.Stdout.String()); got != `{"seq":1,"cpId":"CP1","action":"Heartbeat","payload":{}}` {
		t.Fatalf("record line: %s", got)
	}
}
