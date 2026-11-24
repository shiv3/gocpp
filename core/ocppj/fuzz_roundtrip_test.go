package ocppj

import (
	"testing"
)

// encodeFrame re-encodes a parsed frame with the public encoders.
func encodeFrame(f Frame) ([]byte, error) {
	switch f.Type {
	case Call:
		if f.Signed {
			return EncodeSignedCall(f.MsgID, f.Action, f.Payload)
		}
		return EncodeCall(f.MsgID, f.Action, f.Payload)
	case Send:
		if f.Signed {
			return EncodeSignedSend(f.MsgID, f.Action, f.Payload)
		}
		return EncodeSend(f.MsgID, f.Action, f.Payload)
	case CallResult:
		return EncodeCallResult(f.MsgID, f.Payload)
	case MessageTypeCallError:
		return EncodeCallError(f.MsgID, f.ErrCode, f.ErrDesc, f.ErrData)
	case MessageTypeCallResultError:
		return EncodeCallResultError(f.MsgID, f.ErrCode, f.ErrDesc, f.ErrData)
	}
	return nil, nil
}

// FuzzParseRoundTrip: whatever Parse accepts, re-encoding and parsing again must
// give the same frame (a frame we emit is a frame we understand), and neither
// step may panic. Complements FuzzParseEquivalence, which compares two parsers.
func FuzzParseRoundTrip(f *testing.F) {
	for _, in := range envelopeCorpus {
		f.Add([]byte(in))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		fr, err := Parse(append([]byte(nil), raw...))
		if err != nil {
			return
		}
		// An absent error-details object is sent as {} by design.
		if (fr.Type == MessageTypeCallError || fr.Type == MessageTypeCallResultError) && len(fr.ErrData) == 0 {
			fr.ErrData = []byte("{}")
		}
		out, err := encodeFrame(fr)
		if err != nil {
			t.Fatalf("input %q parsed as %#v but cannot be encoded: %v", raw, fr, err)
		}
		fr2, err := Parse(out)
		if err != nil {
			t.Fatalf("input %q: re-encoded %q does not parse: %v", raw, out, err)
		}
		if !sameFrame(fr, fr2) {
			t.Fatalf("input %q: round trip changed the frame\n first:  %#v\n second: %#v\n wire: %q", raw, fr, fr2, out)
		}
	})
}
