package authn

import (
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shiv3/gocpp/core/auth"
	"golang.org/x/crypto/bcrypt"
)

func TestLoadFileAndVerify(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "creds")
	content := "# comment\n\nCP1:plain:with:colons\nCP2:" + string(hash) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cp, pw string
		ok     bool
	}{
		{"CP1", "plain:with:colons", true},
		{"CP1", "plain", false},
		{"CP2", "s3cret", true},
		{"CP2", string(hash), false},
		{"CP3", "x", false},
	} {
		_, err := creds.Verify(tc.cp, tc.pw)
		if (err == nil) != tc.ok {
			t.Errorf("%s/%s: err %v", tc.cp, tc.pw, err)
		}
	}

	a := Basic(creds)
	r := httptest.NewRequest("GET", "/ocpp/CP1", nil)
	if _, err := a.Authenticate(r, "CP1"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("no credentials: %v", err)
	}
	r.SetBasicAuth("CP1", "plain:with:colons")
	if id, err := All(a).Authenticate(r, "CP1"); err != nil || id.CPID != "CP1" {
		t.Fatalf("good credentials: %+v %v", id, err)
	}
}

func TestLoadFileErrors(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"nocolon": "CP1\n",
		"empty":   "CP1:\n",
		"dup":     "CP1:a\nCP1:b\n",
	} {
		path := filepath.Join(dir, name)
		_ = os.WriteFile(path, []byte(content), 0o600)
		if _, err := LoadFile(path); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
