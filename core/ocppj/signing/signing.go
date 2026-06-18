// Package signing implements OCPP 2.1 Signed Messages (Part 4 Chapter 7): JWS
// signing and verification of OCPP-J payloads.
//
// Wire-format note: OCPP 2.1 Part 4 §7.1 depicts a signed message with an
// {Extension} element, contradicting §4.2.1 (a CALL "always consists of 4
// elements"). This package follows the §4.2 arities — the framing layer emits a
// signed CALL/SEND as [type, msgId, "<Action>-Signed", {JWS}] with no Extension
// element. Only ES256/RS256/RS384 are accepted (Part 4 §7.3).
package signing

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/shiv3/gocpp/core/ocppj"
)

// allowedAlgs is the set of JWS algorithms permitted by OCPP 2.1 Part 4 §7.3.
var allowedAlgs = []jose.SignatureAlgorithm{jose.ES256, jose.RS256, jose.RS384}

// Thumbprint returns the base64url (no padding) SHA-256 of the certificate DER,
// i.e. the JWS x5t#S256 value.
func Thumbprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Signer signs OCPP payloads as Flattened JWS (RFC 7515).
type Signer struct {
	key     crypto.Signer
	alg     jose.SignatureAlgorithm
	x5tS256 string
}

// NewSigner builds a Signer, choosing the algorithm from the key: ECDSA P-256 ->
// ES256, RSA -> RS256. cert supplies the x5t#S256 thumbprint.
func NewSigner(key crypto.Signer, cert *x509.Certificate) (*Signer, error) {
	alg, err := algForKey(key)
	if err != nil {
		return nil, err
	}
	return &Signer{key: key, alg: alg, x5tS256: Thumbprint(cert)}, nil
}

// NewSignerWithAlgorithm is like NewSigner but pins the JWS algorithm (one of
// ES256, RS256, RS384). Use it to select RS384 for an RSA key.
func NewSignerWithAlgorithm(key crypto.Signer, cert *x509.Certificate, alg string) (*Signer, error) {
	ja := jose.SignatureAlgorithm(alg)
	if !algAllowed(ja) {
		return nil, fmt.Errorf("signing: algorithm %q not allowed (use ES256, RS256, or RS384)", alg)
	}
	return &Signer{key: key, alg: ja, x5tS256: Thumbprint(cert)}, nil
}

func algForKey(key crypto.Signer) (jose.SignatureAlgorithm, error) {
	switch pub := key.Public().(type) {
	case *ecdsa.PublicKey:
		if pub.Curve != elliptic.P256() {
			return "", fmt.Errorf("signing: ECDSA key must use P-256 for ES256")
		}
		return jose.ES256, nil
	case *rsa.PublicKey:
		return jose.RS256, nil
	default:
		return "", fmt.Errorf("signing: unsupported key type %T", pub)
	}
}

func algAllowed(a jose.SignatureAlgorithm) bool {
	for _, x := range allowedAlgs {
		if x == a {
			return true
		}
	}
	return false
}

type flattenedJWS struct {
	Protected string `json:"protected"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

// SignPayload signs payload and returns the Flattened JWS JSON Serialization. The
// protected header carries OCPPAction, OCPPMessageTypeId, and x5t#S256.
func (s *Signer) SignPayload(action string, msgType ocppj.MessageType, payload []byte) ([]byte, error) {
	opts := (&jose.SignerOptions{}).
		WithHeader(jose.HeaderKey("OCPPAction"), action).
		WithHeader(jose.HeaderKey("OCPPMessageTypeId"), int(msgType)).
		WithHeader(jose.HeaderKey("x5t#S256"), s.x5tS256)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: s.alg, Key: s.key}, opts)
	if err != nil {
		return nil, fmt.Errorf("signing: new signer: %w", err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		return nil, fmt.Errorf("signing: sign: %w", err)
	}
	compact, err := jws.CompactSerialize()
	if err != nil {
		return nil, fmt.Errorf("signing: serialize: %w", err)
	}
	parts := strings.SplitN(compact, ".", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("signing: unexpected JWS compact form")
	}
	return json.Marshal(flattenedJWS{Protected: parts[0], Payload: parts[1], Signature: parts[2]})
}

// Header holds the parsed protected-header fields of a signed OCPP payload.
type Header struct {
	Alg               string
	X5tS256           string
	OCPPAction        string
	OCPPMessageTypeID int
}

// Verifier verifies signed OCPP payloads against trusted certificates indexed by
// their x5t#S256 thumbprint.
type Verifier struct {
	byThumb map[string]*x509.Certificate
	all     []*x509.Certificate
}

// NewVerifier indexes certs by x5t#S256.
func NewVerifier(certs ...*x509.Certificate) *Verifier {
	v := &Verifier{byThumb: make(map[string]*x509.Certificate, len(certs)), all: certs}
	for _, c := range certs {
		v.byThumb[Thumbprint(c)] = c
	}
	return v
}

// VerifyPayload verifies the Flattened JWS and returns the inner payload plus the
// protected header. When expectedAction != "" or expectedMsgType != 0, the
// corresponding header fields must match. Only ES256/RS256/RS384 are accepted.
func (v *Verifier) VerifyPayload(signed []byte, expectedAction string, expectedMsgType ocppj.MessageType) ([]byte, Header, error) {
	jws, err := jose.ParseSigned(string(signed), allowedAlgs)
	if err != nil {
		return nil, Header{}, fmt.Errorf("signing: parse: %w", err)
	}
	if len(jws.Signatures) != 1 {
		return nil, Header{}, fmt.Errorf("signing: expected exactly one signature")
	}
	hdr := parseHeader(jws.Signatures[0].Protected)

	// Select the verifying certificate by x5t#S256, else try all trusted certs.
	var candidates []*x509.Certificate
	if c, ok := v.byThumb[hdr.X5tS256]; ok {
		candidates = []*x509.Certificate{c}
	} else {
		candidates = v.all
	}
	if len(candidates) == 0 {
		return nil, Header{}, fmt.Errorf("signing: no trusted certificate for x5t#S256 %q", hdr.X5tS256)
	}

	var payload []byte
	verr := fmt.Errorf("signing: no candidate certificate verified the signature")
	for _, c := range candidates {
		if payload, verr = jws.Verify(c.PublicKey); verr == nil {
			break
		}
	}
	if verr != nil {
		return nil, Header{}, fmt.Errorf("signing: verification failed: %w", verr)
	}

	if expectedAction != "" && hdr.OCPPAction != expectedAction {
		return nil, Header{}, fmt.Errorf("signing: header OCPPAction %q != expected %q", hdr.OCPPAction, expectedAction)
	}
	if expectedMsgType != 0 && hdr.OCPPMessageTypeID != int(expectedMsgType) {
		return nil, Header{}, fmt.Errorf("signing: header OCPPMessageTypeId %d != expected %d", hdr.OCPPMessageTypeID, int(expectedMsgType))
	}
	return payload, hdr, nil
}

// UnwrapPayload returns the inner payload of a Flattened JWS WITHOUT verifying the
// signature (Part 4 §7.2: extracting the encapsulated message is mandatory;
// verification is optional).
func UnwrapPayload(signed []byte) ([]byte, error) {
	jws, err := jose.ParseSigned(string(signed), allowedAlgs)
	if err != nil {
		return nil, fmt.Errorf("signing: parse: %w", err)
	}
	return jws.UnsafePayloadWithoutVerification(), nil
}

func parseHeader(p jose.Header) Header {
	h := Header{Alg: p.Algorithm}
	if s, ok := p.ExtraHeaders[jose.HeaderKey("x5t#S256")].(string); ok {
		h.X5tS256 = s
	}
	if s, ok := p.ExtraHeaders[jose.HeaderKey("OCPPAction")].(string); ok {
		h.OCPPAction = s
	}
	if f, ok := p.ExtraHeaders[jose.HeaderKey("OCPPMessageTypeId")].(float64); ok {
		h.OCPPMessageTypeID = int(f)
	}
	return h
}
