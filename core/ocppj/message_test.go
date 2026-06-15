package ocppj

import "testing"

func TestDirection_String(t *testing.T) {
	if SentByCP.String() != "SentByCP" {
		t.Errorf("got %q", SentByCP.String())
	}
	if SentByCSMS.String() != "SentByCSMS" {
		t.Errorf("got %q", SentByCSMS.String())
	}
}

func TestMessage_Fields(t *testing.T) {
	type req struct{ A string }
	type resp struct{ B string }
	m := Message[req, resp]{Action: "Foo", Direction: SentByCP}
	if m.Action != "Foo" || m.Direction != SentByCP {
		t.Fatalf("unexpected message: %+v", m)
	}
}
