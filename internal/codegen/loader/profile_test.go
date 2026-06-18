package loader

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadProfileSendMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
version: v21
profiles:
  Monitoring:
    messages:
      - { name: NotifyPeriodicEventStream, send: NotifyPeriodicEventStream.json, dir: SentByCP }
`), 0o600))

	ps, err := LoadProfile(path)
	require.NoError(t, err)
	m := ps.Profiles["Monitoring"].Messages[0]
	require.Equal(t, "NotifyPeriodicEventStream.json", m.Send)
	require.Empty(t, m.Response)
}

func TestLoadProfile(t *testing.T) {
	p, err := LoadProfile("../profiles/v16.yaml")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	if p.Version != "v16" {
		t.Fatalf("Version = %q, want %q", p.Version, "v16")
	}
	if len(p.Profiles) != 7 {
		t.Fatalf("profiles len = %d, want 7", len(p.Profiles))
	}
	core := p.Profiles["Core"]
	if len(core.Messages) != 16 {
		t.Fatalf("Core messages len = %d, want 16", len(core.Messages))
	}
	m := core.Messages[0]
	if m.Name != "Authorize" {
		t.Fatalf("Name = %q, want %q", m.Name, "Authorize")
	}
	if m.Request != "Authorize.json" {
		t.Fatalf("Request = %q, want %q", m.Request, "Authorize.json")
	}
	if m.Response != "AuthorizeResponse.json" {
		t.Fatalf("Response = %q, want %q", m.Response, "AuthorizeResponse.json")
	}
	if m.Dir != "SentByCP" {
		t.Fatalf("Dir = %q, want %q", m.Dir, "SentByCP")
	}
	smartCharging := p.Profiles["SmartCharging"]
	if len(smartCharging.Messages) != 3 {
		t.Fatalf("SmartCharging messages len = %d, want 3", len(smartCharging.Messages))
	}
	last := smartCharging.Messages[2]
	if last.Name != "SetChargingProfile" {
		t.Fatalf("last SmartCharging message = %q, want SetChargingProfile", last.Name)
	}
	securityExtensions := p.Profiles["SecurityExtensions"]
	if len(securityExtensions.Messages) != 11 {
		t.Fatalf("SecurityExtensions messages len = %d, want 11", len(securityExtensions.Messages))
	}
	if securityExtensions.Messages[0].Name != "CertificateSigned" {
		t.Fatalf("first SecurityExtensions message = %q, want CertificateSigned", securityExtensions.Messages[0].Name)
	}
}
