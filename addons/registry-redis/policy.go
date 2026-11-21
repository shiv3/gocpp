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

// pollEvery is the backstop for a lost pub/sub message: the watcher re-reads
// the policy at this interval (docs/design/gateway.md 5.1.2: push with a 5 s
// poll as backstop).
const pollEvery = 5 * time.Second

var (
	_ storage.PolicyStore   = (*Registry)(nil)
	_ storage.PolicyHistory = (*Registry)(nil)
)

func (r *Registry) pollEvery() time.Duration {
	if r.policyPoll > 0 {
		return r.policyPoll
	}
	return pollEvery
}

// The three policy keys are used by one script, so on Redis Cluster they must
// hash to one slot (CROSSSLOT otherwise): a shared {policy} hash tag.
func (r *Registry) policyKey() string  { return r.prefix + ":{policy}" }
func (r *Registry) policyHist() string { return r.prefix + ":{policy}:h" }
func (r *Registry) policyChan() string { return r.prefix + ":{policy}:ch" }

// KEYS: policy, history, channel. ARGV: prevRev, rev, data, nowMillis, limit.
// Returns 1 when stored, 0 on a CAS miss.
var putPolicyScript = redis.NewScript(`
local cur = redis.call('HGET', KEYS[1], 'rev') or '0'
if tonumber(cur) ~= tonumber(ARGV[1]) then return 0 end
redis.call('HSET', KEYS[1], 'rev', ARGV[2], 'data', ARGV[3])
redis.call('LPUSH', KEYS[2], cjson.encode({rev = tonumber(ARGV[2]), data = ARGV[3], at = tonumber(ARGV[4])}))
redis.call('LTRIM', KEYS[2], 0, tonumber(ARGV[5]) - 1)
redis.call('PUBLISH', KEYS[3], ARGV[2])
return 1
`)

// PutPolicy implements storage.PolicyStore.
func (r *Registry) PutPolicy(ctx context.Context, prevRev, rev uint64, data []byte) (bool, error) {
	if rev <= prevRev {
		return false, storage.ErrPolicyRevision
	}
	n, err := putPolicyScript.Run(ctx, r.rdb, []string{r.policyKey(), r.policyHist(), r.policyChan()},
		strconv.FormatUint(prevRev, 10), strconv.FormatUint(rev, 10), string(data),
		time.Now().UnixMilli(), storage.PolicyHistoryLimit).Int()
	if err != nil {
		return false, fmt.Errorf("registryredis: put policy: %w", err)
	}
	return n == 1, nil
}

// Policy implements storage.PolicyStore.
func (r *Registry) Policy(ctx context.Context) (uint64, []byte, error) {
	f, err := r.rdb.HGetAll(ctx, r.policyKey()).Result()
	if err != nil {
		return 0, nil, fmt.Errorf("registryredis: policy: %w", err)
	}
	if len(f) == 0 {
		return 0, nil, nil
	}
	rev, err := strconv.ParseUint(f["rev"], 10, 64)
	if err != nil {
		return 0, nil, fmt.Errorf("registryredis: policy revision %q: %w", f["rev"], err)
	}
	return rev, []byte(f["data"]), nil
}

// PolicyHistory implements storage.PolicyHistory.
func (r *Registry) PolicyHistory(ctx context.Context) ([]storage.PolicyEntry, error) {
	items, err := r.rdb.LRange(ctx, r.policyHist(), 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("registryredis: policy history: %w", err)
	}
	out := make([]storage.PolicyEntry, 0, len(items))
	for _, it := range items {
		var e struct {
			Rev  uint64 `json:"rev"`
			Data string `json:"data"`
			At   int64  `json:"at"`
		}
		if json.Unmarshal([]byte(it), &e) != nil {
			continue
		}
		out = append(out, storage.PolicyEntry{Rev: e.Rev, Data: []byte(e.Data), At: time.UnixMilli(e.At)})
	}
	return out, nil
}

// WatchPolicy implements storage.PolicyStore: it subscribes to the policy
// channel (go-redis resubscribes after a connection loss) and also polls, so a
// missed message or an outage only delays the update by the poll interval.
func (r *Registry) WatchPolicy(ctx context.Context, fn func(uint64, []byte)) error {
	sub := r.rdb.Subscribe(ctx, r.policyChan())
	defer func() { _ = sub.Close() }()
	msgs := sub.Channel(redis.WithChannelHealthCheckInterval(time.Second))
	var seen uint64
	check := func() {
		rev, data, err := r.Policy(ctx)
		if err != nil || rev <= seen {
			return
		}
		seen = rev
		fn(rev, data)
	}
	check()
	tick := time.NewTicker(r.pollEvery())
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-msgs:
			if !ok {
				return errors.New("registryredis: policy subscription closed")
			}
			check()
		case <-tick.C:
			check()
		}
	}
}
