package ocppj

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// envelopeCorpus is shared by the equivalence tests and seeds the fuzzers: the
// shapes the parser accepts, the lenient ones, every malformed shape the
// interop parity cases send, and encoding/json corner cases.
var envelopeCorpus = []string{
	`[2,"abc","BootNotification",{"chargePointVendor":"X"}]`,
	`[2,"id","Heartbeat",{}]`,
	`[2,"id","Heartbeat",null]`,
	`[2,"id","Heartbeat",[]]`,
	`[2,"","Heartbeat",{}]`,
	`[2,"id","BootNotification-Signed",{"protected":"p","payload":"pl","signature":"s"}]`,
	`[2,"id","-Signed",{}]`,
	`[3,"abc",{"status":"Accepted"}]`,
	`[3,"a",{"x":1},"extra"]`,
	`[3,"a"]`,
	`[3,"nope",{}]`,
	`[4,"abc","NotImplemented","not impl",{}]`,
	`[4,"a","InternalError","boom"]`,
	`[4,"a","InternalError",42,{}]`,
	`[4,"a",1,"x",{}]`,
	`[4,"a","Code"]`,
	`[4,"nope","GenericError","d"]`,
	`[4,"nope","GenericError","d",{},{}]`,
	`[5,"abc","FormatViolation","bad result",{"k":"v"}]`,
	`[5,"a","FormatViolation","bad"]`,
	`[6,"abc","NotifyPeriodicEventStream",{"id":123}]`,
	`[6,"abc","Action"]`,
	`[7,"abc",{}]`,
	`[9,"id","Heartbeat",{}]`,
	`[0,"id",{}]`,
	`[2.0,"id","Heartbeat",{}]`,
	`[2e0,"id","Heartbeat",{}]`,
	`[ 2 , "id" , "Heartbeat" , { } ]`,
	"\t[\n2,\r\"id\",\"Heartbeat\",{}\n]\n",
	`[2,"id","Heartbeat",{},"extra"]`,
	`[2,"id","Heartbeat"]`,
	`[2,"id",5,{}]`,
	`[2,5,"Heartbeat",{}]`,
	`[2,"id"]`,
	`[2]`,
	`[]`,
	`{}`,
	`null`,
	``,
	`[`,
	`[2,"id","Heartbeat",{}`,
	`[2,"id","Heartbeat",{}]]`,
	`[2,"id","Heartbeat",{}] x`,
	`[2,"id","Heartbeat",{},]`,
	`[2,"i\"d","Heartbeat",{}]`,
	`[2,"i\u0064","Heart\u0062eat",{}]`,
	"[2,\"id\",\"Heartbeat\",{\"a\":\"\\\\\"}]",
	`[2,"id","Heartbeat",{"a":"]\"[,"}]`,
	`[2,"id","Heartbeat",[[[],[{}]],"x"]]`,
	`[2,"id","Heartbeat",-1.5e+3]`,
	`[2,"id","Heartbeat",true]`,
	`[2,"id","Heartbeat","str"]`,
	"[2,\"i\xffd\",\"Heartbeat\",{}]",
	"[2,\"id\",\"Heart\xc3\x28\",{}]",
	"[2,\"id\",\"H\u00e9\u2028\",{}]",
	"[2,\"id\",\"Heartbeat\",{\"k\":\"\xff\"}]",
	"[2,\"i\x01d\",\"Heartbeat\",{}]",
	`[2,"id","Heartbeat",{"a":1,"a":2}]`,
	`[2,"id","Heartbeat",{},{},{},{},{},{}]`,
	`[3,"a",{},{},{},{},{}]`,
	`[3,"a",1,2,3,4,5,6,7]`,
	`[4,"a","c","d",1,2,3,4]`,
	`[03,"id",{}]`,
	`[-2,"id",{}]`,
	`[22,"id",{}]`,
	`["2","id","Heartbeat",{}]`,
	`[2,"id",null,{}]`,
	`[2,null,"Heartbeat",{}]`,
	`[3,"` + strings.Repeat("x", 40) + `",{}]`,
	strings.Repeat("[", 200),
}

// sameFrame compares two Frames; nil and empty byte slices are the same payload.
func sameFrame(a, b Frame) bool {
	return a.Type == b.Type && a.Signed == b.Signed && a.MsgID == b.MsgID && a.Action == b.Action &&
		a.ErrCode == b.ErrCode && a.ErrDesc == b.ErrDesc &&
		bytes.Equal(a.Payload, b.Payload) && bytes.Equal(a.ErrData, b.ErrData) &&
		(a.Payload == nil) == (b.Payload == nil) && (a.ErrData == nil) == (b.ErrData == nil)
}

func checkParseEquivalent(t testing.TB, raw []byte) {
	t.Helper()
	// Parse hands out sub-slices of its argument; give each implementation its own
	// copy so one cannot influence the other.
	fast, ferr := Parse(append([]byte(nil), raw...))
	slow, serr := parseSlow(append([]byte(nil), raw...))
	if (ferr == nil) != (serr == nil) {
		t.Fatalf("input %q: Parse err=%v, parseSlow err=%v", raw, ferr, serr)
	}
	if ferr != nil {
		if reflect.TypeOf(ferr) != reflect.TypeOf(serr) || !reflect.DeepEqual(ferr, serr) {
			t.Fatalf("input %q: error mismatch\n fast: %#v\n slow: %#v", raw, ferr, serr)
		}
		var uf, us *UnknownMessageTypeError
		if errors.As(ferr, &uf) != errors.As(serr, &us) {
			t.Fatalf("input %q: unknown-type classification differs", raw)
		}
		return
	}
	if !sameFrame(fast, slow) {
		t.Fatalf("input %q: frame mismatch\n fast: %#v\n slow: %#v", raw, fast, slow)
	}
}

func TestParseEquivalence_Corpus(t *testing.T) {
	for _, in := range envelopeCorpus {
		checkParseEquivalent(t, []byte(in))
	}
}

// FuzzParseEquivalence: the scanner fast path must agree with the reflective
// parser on every input, error type and fields included.
func FuzzParseEquivalence(f *testing.F) {
	for _, in := range envelopeCorpus {
		f.Add([]byte(in))
	}
	f.Fuzz(func(t *testing.T, raw []byte) { checkParseEquivalent(t, raw) })
}

func TestParse_FastPathTaken(t *testing.T) {
	for _, in := range []string{
		`[2,"id","Heartbeat",{}]`, `[3,"id",{}]`, `[4,"id","C","d",{}]`, `[5,"id","C","d"]`, `[6,"id","A",{}]`,
		` [ 2 , "id" , "A" , {"k":[1,"]"]} ] `,
	} {
		if _, ok := parseFast([]byte(in)); !ok {
			t.Errorf("expected fast path for %q", in)
		}
	}
	for _, in := range []string{`[2,"i\"d","A",{}]`, `[2,"id","A"]`, `[7,"id",{}]`, `[4,"id",1,"d",{}]`, "[2,\"\xff\",\"A\",{}]"} {
		if _, ok := parseFast([]byte(in)); ok {
			t.Errorf("expected fallback for %q", in)
		}
	}
}

func TestParse_PayloadAliasesInput(t *testing.T) {
	raw := []byte(`[2,"id","Heartbeat",{"a":1}]`)
	f, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Payload) == 0 || &f.Payload[0] != &raw[strings.Index(string(raw), "{")] {
		t.Fatal("fast path payload should be a sub-slice of the input")
	}
	// The strings are copies: they survive the caller recycling the buffer.
	for i := range raw {
		raw[i] = 'x'
	}
	if f.MsgID != "id" || f.Action != "Heartbeat" {
		t.Fatalf("strings must not alias the input: %+v", f)
	}
}

func TestParse_Allocs(t *testing.T) {
	raw := []byte(`[2,"01JABCDEFGHJKMNPQRSTVWXYZ0","StatusNotification",{"connectorId":1,"status":"Available"}]`)
	// AllocsPerRun counts mallocs process-wide, so a goroutine from an earlier
	// test or the race detector's own bookkeeping can add to a single
	// measurement. The code's real cost is the minimum over a few runs.
	best := -1.0
	for i := 0; i < 10; i++ {
		runtime.GC()
		n := testing.AllocsPerRun(100, func() { _, _ = Parse(raw) })
		if best < 0 || n < best {
			best = n
		}
	}
	if best > 2 {
		t.Fatalf("Parse(CALL) allocates %.0f times, want <= 2", best)
	}
}

// ---------------------------------------------------------------- encoder

type encCase struct {
	msgID, action, code, desc string
	payload                   []byte
}

func encodeReference(t testing.TB, c encCase) map[string][]byte {
	t.Helper()
	ref := func(elems ...any) []byte {
		b, err := encodeArray(elems...)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	det := c.payload
	if len(det) == 0 {
		det = []byte("{}")
	}
	return map[string][]byte{
		"call":        ref(int(Call), c.msgID, c.action, json.RawMessage(c.payload)),
		"result":      ref(int(CallResult), c.msgID, json.RawMessage(c.payload)),
		"error":       ref(int(MessageTypeCallError), c.msgID, c.code, c.desc, json.RawMessage(det)),
		"send":        ref(int(Send), c.msgID, c.action, json.RawMessage(c.payload)),
		"resulterror": ref(int(MessageTypeCallResultError), c.msgID, c.code, c.desc, json.RawMessage(det)),
		"signedcall":  ref(int(Call), c.msgID, c.action+signedSuffix, json.RawMessage(c.payload)),
		"signedsend":  ref(int(Send), c.msgID, c.action+signedSuffix, json.RawMessage(c.payload)),
	}
}

func checkEncodeEquivalent(t testing.TB, c encCase) {
	t.Helper()
	want := encodeReference(t, c)
	got := map[string][]byte{}
	var err error
	if got["call"], err = EncodeCall(c.msgID, c.action, c.payload); err != nil {
		t.Fatal(err)
	}
	got["result"], _ = EncodeCallResult(c.msgID, c.payload)
	got["error"], _ = EncodeCallError(c.msgID, c.code, c.desc, c.payload)
	got["send"], _ = EncodeSend(c.msgID, c.action, c.payload)
	got["resulterror"], _ = EncodeCallResultError(c.msgID, c.code, c.desc, c.payload)
	got["signedcall"], _ = EncodeSignedCall(c.msgID, c.action, c.payload)
	got["signedsend"], _ = EncodeSignedSend(c.msgID, c.action, c.payload)
	for k, w := range want {
		if !bytes.Equal(got[k], w) {
			t.Fatalf("%s %+v:\n got  %q\n want %q", k, c, got[k], w)
		}
	}
}

var encodeCorpus = []encCase{
	{"id", "Heartbeat", "NotImplemented", "unknown action", []byte(`{}`)},
	{"01JABCDEFGHJKMNPQRSTVWXYZ0", "StatusNotification", "InternalError", "boom", []byte(`{"a":[1,2,{"b":"c"}]}`)},
	{"", "", "", "", nil},
	{"i\"d", "A\\B", "c<o>de", "d&e", []byte(`null`)},
	{"id\n", "Act\tion", "\x00", "\u2028", []byte(`[]`)},
	{"h\u00e9llo", "\u65e5\u672c\u8a9e", "\xff\xfe", "d\x7fx", []byte(`"s"`)},
	{strings.Repeat("x", 300), strings.Repeat("y", 300), "c", strings.Repeat("<", 50), []byte(strings.Repeat("[", 3))},
}

func TestEncodeEquivalence_Corpus(t *testing.T) {
	for _, c := range encodeCorpus {
		checkEncodeEquivalent(t, c)
	}
}

// FuzzEncodeEquivalence: appendFrame must write exactly the bytes encodeArray did.
func FuzzEncodeEquivalence(f *testing.F) {
	for _, c := range encodeCorpus {
		f.Add(c.msgID, c.action, c.code, c.desc, c.payload)
	}
	f.Fuzz(func(t *testing.T, msgID, action, code, desc string, payload []byte) {
		checkEncodeEquivalent(t, encCase{msgID, action, code, desc, payload})
	})
}

func TestEncode_Allocs(t *testing.T) {
	payload := []byte(`{"currentTime":"2026-11-21T01:02:03.000Z"}`)
	if n := testing.AllocsPerRun(100, func() { _, _ = EncodeCallResult("01JABCDEFGHJKMNPQRSTVWXYZ0", payload) }); n > 1 {
		t.Fatalf("EncodeCallResult allocates %.0f times, want 1", n)
	}
}
