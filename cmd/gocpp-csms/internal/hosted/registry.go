// Package hosted turns gocpp-csms serve mode into a hosted test CSMS: an
// instance in open mode (any charge point id connects, up to a connection
// limit answered with HTTP 429) or private mode (only registered charge
// points, each with its own server-generated HTTP Basic password stored as a
// salted hash), a registry REST API on the control listener, JSON-file
// persistence and an optional instance path segment
// (<path-prefix><instanceId>/<cpId>).
package hosted

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

// Modes of an instance.
const (
	// ModeOpen accepts any charge point id; registering is refused.
	ModeOpen = "open"
	// ModePrivate accepts registered charge points with their password only.
	ModePrivate = "private"
)

// Registry errors.
var (
	ErrOpenMode    = errors.New("the instance is in open mode; switch it to private mode to register charge points")
	ErrExists      = errors.New("charge point already registered")
	ErrNotFound    = errors.New("charge point not registered")
	ErrInvalidID   = errors.New("cpId must be 1 to 48 printable characters without '/', ':' or spaces")
	ErrDisplayName = errors.New("displayName is required")
)

// bcryptCost is the cost of the stored password hashes (lowered in tests).
var bcryptCost = bcrypt.DefaultCost

// Registration is a registered charge point as stored (no password).
type Registration struct {
	CPID              string    `json:"cpId"`
	DisplayName       string    `json:"displayName"`
	PasswordHash      string    `json:"passwordHash"`
	CreatedAt         time.Time `json:"createdAt"`
	PasswordChangedAt time.Time `json:"passwordChangedAt"`
}

// state is the persisted document.
type state struct {
	Version      int            `json:"version"`
	Mode         string         `json:"mode"`
	InstanceID   string         `json:"instanceId,omitempty"`
	ChargePoints []Registration `json:"chargePoints"`
}

// Registry holds the instance mode and the registered charge points. With a
// path every change is written to it atomically (temporary file + rename)
// before it takes effect.
type Registry struct {
	mu    sync.RWMutex
	path  string
	mode  string
	id    string
	cps   map[string]Registration
	clock func() time.Time
}

// Open loads the registry from path ("" = in memory only). A missing file
// is an empty registry in open mode.
func Open(path string) (*Registry, error) {
	r := &Registry{path: path, mode: ModeOpen, cps: map[string]Registration{}, clock: time.Now}
	if path == "" {
		return r, nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if st.Version != 1 {
		return nil, fmt.Errorf("%s: unsupported version %d", path, st.Version)
	}
	switch st.Mode {
	case ModeOpen, ModePrivate:
		r.mode = st.Mode
	case "":
	default:
		return nil, fmt.Errorf("%s: unknown mode %q", path, st.Mode)
	}
	r.id = st.InstanceID
	for _, cp := range st.ChargePoints {
		if ValidCPID(cp.CPID) != nil || cp.PasswordHash == "" {
			return nil, fmt.Errorf("%s: invalid charge point entry %q", path, cp.CPID)
		}
		r.cps[cp.CPID] = cp
	}
	return r, nil
}

// ValidCPID checks a charge point id given for registration.
func ValidCPID(id string) error {
	if id == "" || len(id) > 48 {
		return ErrInvalidID
	}
	for _, c := range id {
		if c == '/' || c == ':' || unicode.IsSpace(c) || !unicode.IsPrint(c) {
			return ErrInvalidID
		}
	}
	return nil
}

// Mode returns the current mode.
func (r *Registry) Mode() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mode
}

// InstanceID returns the persisted instance id ("" if none).
func (r *Registry) InstanceID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.id
}

// SetInstanceID stores the instance id (persisted).
func (r *Registry) SetInstanceID(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.id == id {
		return nil
	}
	return r.commit(r.mode, id, r.cps)
}

// SetMode switches the mode (persisted). Registrations are kept in open
// mode and apply again when switching back to private mode.
func (r *Registry) SetMode(mode string) error {
	if mode != ModeOpen && mode != ModePrivate {
		return fmt.Errorf("mode must be %q or %q", ModeOpen, ModePrivate)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode == mode {
		return nil
	}
	return r.commit(mode, r.id, r.cps)
}

// Register adds a charge point and returns its generated password (the only
// time it is available).
func (r *Registry) Register(cpID, displayName string) (Registration, string, error) {
	if err := ValidCPID(cpID); err != nil {
		return Registration{}, "", err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return Registration{}, "", ErrDisplayName
	}
	password, hash, err := newPassword()
	if err != nil {
		return Registration{}, "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.mode != ModePrivate {
		return Registration{}, "", ErrOpenMode
	}
	if _, ok := r.cps[cpID]; ok {
		return Registration{}, "", ErrExists
	}
	now := r.clock().UTC()
	reg := Registration{CPID: cpID, DisplayName: displayName, PasswordHash: hash, CreatedAt: now, PasswordChangedAt: now}
	next := cloneMap(r.cps)
	next[cpID] = reg
	if err := r.commit(r.mode, r.id, next); err != nil {
		return Registration{}, "", err
	}
	return reg, password, nil
}

// Rotate replaces a charge point's password and returns the new one. A live
// session is not affected; the next connection needs the new password.
func (r *Registry) Rotate(cpID string) (Registration, string, error) {
	password, hash, err := newPassword()
	if err != nil {
		return Registration{}, "", err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	reg, ok := r.cps[cpID]
	if !ok {
		return Registration{}, "", ErrNotFound
	}
	reg.PasswordHash = hash
	reg.PasswordChangedAt = r.clock().UTC()
	next := cloneMap(r.cps)
	next[cpID] = reg
	if err := r.commit(r.mode, r.id, next); err != nil {
		return Registration{}, "", err
	}
	return reg, password, nil
}

// Delete removes a registration. A live session is not dropped.
func (r *Registry) Delete(cpID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cps[cpID]; !ok {
		return ErrNotFound
	}
	next := cloneMap(r.cps)
	delete(next, cpID)
	return r.commit(r.mode, r.id, next)
}

// Clear removes every registration and returns how many there were; the
// mode and instance id stay.
func (r *Registry) Clear() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.cps)
	if n == 0 {
		return 0, nil
	}
	if err := r.commit(r.mode, r.id, map[string]Registration{}); err != nil {
		return 0, err
	}
	return n, nil
}

// List returns the registrations sorted by id.
func (r *Registry) List() []Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Registration, 0, len(r.cps))
	for _, cp := range r.cps {
		out = append(out, cp)
	}
	slices.SortFunc(out, func(a, b Registration) int { return strings.Compare(a.CPID, b.CPID) })
	return out
}

// Lookup returns a registration.
func (r *Registry) Lookup(cpID string) (Registration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.cps[cpID]
	return reg, ok
}

// Verify checks a charge point password against its stored hash.
func (r *Registry) Verify(cpID, password string) bool {
	reg, ok := r.Lookup(cpID)
	if !ok {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(reg.PasswordHash), []byte(password)) == nil
}

// commit persists the next state, then makes it current. Callers hold mu.
func (r *Registry) commit(mode, id string, cps map[string]Registration) error {
	if r.path != "" {
		st := state{Version: 1, Mode: mode, InstanceID: id, ChargePoints: make([]Registration, 0, len(cps))}
		for _, cp := range cps {
			st.ChargePoints = append(st.ChargePoints, cp)
		}
		slices.SortFunc(st.ChargePoints, func(a, b Registration) int { return strings.Compare(a.CPID, b.CPID) })
		b, err := json.MarshalIndent(st, "", "  ")
		if err != nil {
			return err
		}
		if err := writeFileAtomic(r.path, append(b, '\n')); err != nil {
			return fmt.Errorf("persist registry: %w", err)
		}
	}
	r.mode, r.id, r.cps = mode, id, cps
	return nil
}

// writeFileAtomic writes data to a temporary file next to path, syncs it and
// renames it over path, so a reader or a crash sees the old or the new file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func cloneMap(m map[string]Registration) map[string]Registration {
	out := make(map[string]Registration, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// newPassword returns a random 32-character password (within the 16 to 40
// characters OCPP allows for a Basic Auth password) and its bcrypt hash.
func newPassword() (string, string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	password := hex.EncodeToString(b)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", "", err
	}
	return password, string(hash), nil
}

// NewInstanceID returns a random instance id (24 bytes, URL-safe base64).
func NewInstanceID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
