package cp

import (
	"testing"

	"github.com/shiv3/gocpp/core/ocppj/signing"
	"github.com/stretchr/testify/require"
)

func TestWithSignerSetsDispatcherSigner(t *testing.T) {
	cfg := defaultClientConfig()
	s := &signing.Signer{}
	WithSigner(s).apply(&cfg)
	require.Same(t, s, cfg.dispatcher.Signer)
}

func TestWithVerifierSetsDispatcherVerifier(t *testing.T) {
	cfg := defaultClientConfig()
	v := signing.NewVerifier()
	WithVerifier(v).apply(&cfg)
	require.Same(t, v, cfg.dispatcher.Verifier)
}

func TestWithRequireSignatureSetsFlag(t *testing.T) {
	cfg := defaultClientConfig()
	WithRequireSignature(true).apply(&cfg)
	require.True(t, cfg.dispatcher.RequireSignatureVerification)
}
