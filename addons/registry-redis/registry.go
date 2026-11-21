package registryredis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shiv3/gocpp/core/storage"
)

const (
	defaultPrefix    = "gocpp:sess"
	defaultRecordTTL = 24 * time.Hour
	defaultEpochTTL  = 30 * 24 * time.Hour
)

// Registry is a storage.SessionRegistry backed by Redis.
type Registry struct {
	rdb       redis.UniversalClient
	prefix    string
	recordTTL time.Duration
	epochTTL  time.Duration
}

var _ storage.SessionRegistry = (*Registry)(nil)

// Option configures a Registry.
type Option func(*Registry)

// WithKeyPrefix sets the key prefix (default "gocpp:sess").
func WithKeyPrefix(p string) Option { return func(r *Registry) { r.prefix = p } }

// WithRecordTTL sets the expiry of a charge point's record (default 24h). It only
// garbage-collects records nobody cleaned up; liveness comes from the gateway
// lease. A gateway's reconcile pass refreshes it.
func WithRecordTTL(d time.Duration) Option { return func(r *Registry) { r.recordTTL = d } }

// WithEpochTTL sets how long a charge point's epoch counter outlives its last
// claim (default 30 days). The counter must outlive any record or a stale
// writer could meet a reused epoch.
func WithEpochTTL(d time.Duration) Option { return func(r *Registry) { r.epochTTL = d } }

// New returns a Registry using rdb.
func New(rdb redis.UniversalClient, opts ...Option) *Registry {
	r := &Registry{rdb: rdb, prefix: defaultPrefix, recordTTL: defaultRecordTTL, epochTTL: defaultEpochTTL}
	for _, o := range opts {
		o(r)
	}
	return r
}

func (r *Registry) epochKey(cp string) string { return r.prefix + ":{" + cp + "}:e" }
func (r *Registry) recKey(cp string) string   { return r.prefix + ":{" + cp + "}:r" }
func (r *Registry) leaseKey(gw string) string { return r.prefix + ":gw:" + gw }

// sessionData is the part of a Session that Redis stores opaquely.
type sessionData struct {
	Version     string            `json:"v,omitempty"`
	ConnectedAt time.Time         `json:"t,omitempty"`
	Meta        map[string]string `json:"m,omitempty"`
}

func encodeData(s storage.Session) (string, error) {
	b, err := json.Marshal(sessionData{Version: s.Version, ConnectedAt: s.ConnectedAt, Meta: s.Meta})
	return string(b), err
}

func decodeRecord(cp string, f map[string]string) (storage.Session, error) {
	s := storage.Session{CPID: cp, GatewayID: f["gw"], Boot: f["boot"]}
	e, err := strconv.ParseUint(f["epoch"], 10, 64)
	if err != nil {
		return s, fmt.Errorf("registryredis: bad epoch %q for %q", f["epoch"], cp)
	}
	s.Epoch = e
	if d := f["data"]; d != "" {
		var sd sessionData
		if err := json.Unmarshal([]byte(d), &sd); err != nil {
			return s, fmt.Errorf("registryredis: bad session data for %q: %w", cp, err)
		}
		s.Version, s.ConnectedAt, s.Meta = sd.Version, sd.ConnectedAt, sd.Meta
	}
	return s, nil
}

// KEYS: epoch, record. ARGV: gw, boot, data, recordTTL(s), epochTTL(s).
var claimScript = redis.NewScript(`
local e = redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], ARGV[5])
local prev = redis.call('HGETALL', KEYS[2])
redis.call('HSET', KEYS[2], 'gw', ARGV[1], 'boot', ARGV[2], 'epoch', tostring(e), 'data', ARGV[3])
redis.call('EXPIRE', KEYS[2], ARGV[4])
return {e, prev}
`)

// KEYS: record. ARGV: epoch.
var releaseScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], 'epoch') == ARGV[1] then
  redis.call('DEL', KEYS[1])
  return 1
end
return 0
`)

// KEYS: epoch, record. ARGV: epoch, gw, boot, data, recordTTL(s), epochTTL(s).
var reassertScript = redis.NewScript(`
local want = tonumber(ARGV[1])
local cur = redis.call('HGET', KEYS[2], 'epoch')
if cur then
  local c = tonumber(cur)
  if c > want then return 0 end
  if c == want then
    if redis.call('HGET', KEYS[2], 'gw') ~= ARGV[2] or redis.call('HGET', KEYS[2], 'boot') ~= ARGV[3] then
      return 0
    end
    redis.call('EXPIRE', KEYS[2], ARGV[5])
    return 1
  end
end
local ec = tonumber(redis.call('GET', KEYS[1]) or '0')
if ec < want then redis.call('SET', KEYS[1], ARGV[1]) end
redis.call('EXPIRE', KEYS[1], ARGV[6])
redis.call('HSET', KEYS[2], 'gw', ARGV[2], 'boot', ARGV[3], 'epoch', ARGV[1], 'data', ARGV[4])
redis.call('EXPIRE', KEYS[2], ARGV[5])
return 1
`)

func secs(d time.Duration) int64 { return max(int64(d/time.Second), 1) }

// Claim implements storage.SessionRegistry.
func (r *Registry) Claim(ctx context.Context, s storage.Session) (uint64, *storage.Session, error) {
	data, err := encodeData(s)
	if err != nil {
		return 0, nil, err
	}
	res, err := claimScript.Run(ctx, r.rdb, []string{r.epochKey(s.CPID), r.recKey(s.CPID)},
		s.GatewayID, s.Boot, data, secs(r.recordTTL), secs(r.epochTTL)).Slice()
	if err != nil {
		return 0, nil, fmt.Errorf("registryredis: claim %q: %w", s.CPID, err)
	}
	if len(res) != 2 {
		return 0, nil, errors.New("registryredis: unexpected claim reply")
	}
	epoch, ok := res[0].(int64)
	if !ok || epoch <= 0 {
		return 0, nil, errors.New("registryredis: unexpected claim epoch")
	}
	flat, _ := res[1].([]any)
	if len(flat) == 0 {
		return uint64(epoch), nil, nil
	}
	f := make(map[string]string, len(flat)/2)
	for i := 0; i+1 < len(flat); i += 2 {
		k, _ := flat[i].(string)
		v, _ := flat[i+1].(string)
		f[k] = v
	}
	prev, err := decodeRecord(s.CPID, f)
	if err != nil {
		return uint64(epoch), nil, nil // the claim itself succeeded
	}
	return uint64(epoch), &prev, nil
}

// Release implements storage.SessionRegistry.
func (r *Registry) Release(ctx context.Context, cpID string, epoch uint64) (bool, error) {
	n, err := releaseScript.Run(ctx, r.rdb, []string{r.recKey(cpID)}, strconv.FormatUint(epoch, 10)).Int()
	if err != nil {
		return false, fmt.Errorf("registryredis: release %q: %w", cpID, err)
	}
	return n == 1, nil
}

// Reassert implements storage.SessionRegistry.
func (r *Registry) Reassert(ctx context.Context, s storage.Session) (bool, error) {
	data, err := encodeData(s)
	if err != nil {
		return false, err
	}
	n, err := reassertScript.Run(ctx, r.rdb, []string{r.epochKey(s.CPID), r.recKey(s.CPID)},
		strconv.FormatUint(s.Epoch, 10), s.GatewayID, s.Boot, data, secs(r.recordTTL), secs(r.epochTTL)).Int()
	if err != nil {
		return false, fmt.Errorf("registryredis: reassert %q: %w", s.CPID, err)
	}
	return n == 1, nil
}

// Lookup implements storage.SessionRegistry.
func (r *Registry) Lookup(ctx context.Context, cpID string) (storage.Session, bool, error) {
	got, err := r.LookupMany(ctx, []string{cpID})
	if err != nil || len(got) == 0 {
		return storage.Session{}, false, err
	}
	return got[0], true, nil
}

// LookupMany implements storage.SessionRegistry: one pipeline for the records,
// one for the leases of the gateways they name.
func (r *Registry) LookupMany(ctx context.Context, cpIDs []string) ([]storage.Session, error) {
	if len(cpIDs) == 0 {
		return nil, nil
	}
	pipe := r.rdb.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(cpIDs))
	for i, id := range cpIDs {
		cmds[i] = pipe.HGetAll(ctx, r.recKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("registryredis: lookup: %w", err)
	}
	var recs []storage.Session
	gws := map[string]struct{}{}
	for i, c := range cmds {
		f := c.Val()
		if len(f) == 0 {
			continue
		}
		s, err := decodeRecord(cpIDs[i], f)
		if err != nil {
			continue
		}
		recs = append(recs, s)
		if s.Boot != "" {
			gws[s.GatewayID] = struct{}{}
		}
	}
	boots, err := r.leaseBoots(ctx, gws)
	if err != nil {
		return nil, err
	}
	out := recs[:0]
	for _, s := range recs {
		if s.Boot == "" || boots[s.GatewayID] == s.Boot {
			out = append(out, s)
		}
	}
	return out, nil
}

func (r *Registry) leaseBoots(ctx context.Context, gws map[string]struct{}) (map[string]string, error) {
	boots := make(map[string]string, len(gws))
	if len(gws) == 0 {
		return boots, nil
	}
	pipe := r.rdb.Pipeline()
	cmds := make(map[string]*redis.StringCmd, len(gws))
	for g := range gws {
		cmds[g] = pipe.Get(ctx, r.leaseKey(g))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("registryredis: lease lookup: %w", err)
	}
	for g, c := range cmds {
		v, err := c.Result()
		if err != nil {
			continue // redis.Nil: no lease
		}
		var info storage.GatewayInfo
		if json.Unmarshal([]byte(v), &info) == nil {
			boots[g] = info.Boot
		}
	}
	return boots, nil
}

type leaseInfo struct {
	ID       string `json:"id"`
	Boot     string `json:"boot"`
	Address  string `json:"addr,omitempty"`
	State    string `json:"state,omitempty"`
	Sessions int    `json:"sessions"`
}

// Heartbeat implements storage.SessionRegistry.
func (r *Registry) Heartbeat(ctx context.Context, g storage.GatewayInfo, ttl time.Duration) error {
	b, err := json.Marshal(leaseInfo{ID: g.ID, Boot: g.Boot, Address: g.Address, State: g.State, Sessions: g.Sessions})
	if err != nil {
		return err
	}
	if err := r.rdb.Set(ctx, r.leaseKey(g.ID), b, ttl).Err(); err != nil {
		return fmt.Errorf("registryredis: heartbeat: %w", err)
	}
	return nil
}

// Gateways implements storage.SessionRegistry.
func (r *Registry) Gateways(ctx context.Context) ([]storage.GatewayInfo, error) {
	var out []storage.GatewayInfo
	iter := r.rdb.Scan(ctx, 0, r.prefix+":gw:*", 100).Iterator()
	for iter.Next(ctx) {
		v, err := r.rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		var li leaseInfo
		if json.Unmarshal([]byte(v), &li) == nil {
			out = append(out, storage.GatewayInfo{ID: li.ID, Boot: li.Boot, Address: li.Address, State: li.State, Sessions: li.Sessions})
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("registryredis: gateways: %w", err)
	}
	return out, nil
}
