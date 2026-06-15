package loader

import "testing"

func TestLoadProfile(t *testing.T) {
	p, err := LoadProfile("../profiles/v16.yaml")
	if err != nil {
		t.Fatalf("LoadProfile() error = %v", err)
	}
	if p.Version != "v16" {
		t.Fatalf("Version = %q, want %q", p.Version, "v16")
	}
	if len(p.Profiles) != 1 {
		t.Fatalf("profiles len = %d, want 1", len(p.Profiles))
	}
	core := p.Profiles["Core"]
	if len(core.Messages) != 1 {
		t.Fatalf("Core messages len = %d, want 1", len(core.Messages))
	}
	m := core.Messages[0]
	if m.Name != "BootNotification" {
		t.Fatalf("Name = %q, want %q", m.Name, "BootNotification")
	}
	if m.Request != "BootNotification.json" {
		t.Fatalf("Request = %q, want %q", m.Request, "BootNotification.json")
	}
	if m.Response != "BootNotificationResponse.json" {
		t.Fatalf("Response = %q, want %q", m.Response, "BootNotificationResponse.json")
	}
	if m.Dir != "SentByCP" {
		t.Fatalf("Dir = %q, want %q", m.Dir, "SentByCP")
	}
}
