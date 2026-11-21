package rollout

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Action is what the controller did (or, in a dry run, would do).
type Action string

// Actions.
const (
	ActionApply    Action = "apply"    // a stage was written
	ActionHold     Action = "hold"     // no decision: soaking, warming up, too few samples, gateways lagging
	ActionAdvance  Action = "advance"  // the gates passed and the soak time is over
	ActionRollback Action = "rollback" // a gate failed or the guard tripped
	ActionDone     Action = "done"     // the last stage passed: ready to promote
	ActionYield    Action = "yield"    // somebody else changed the policy: the controller stops
	ActionError    Action = "error"
)

// Decision is one controller decision, passed to the Notifier.
type Decision struct {
	Time     time.Time    `json:"time"`
	Action   Action       `json:"action"`
	Stage    int          `json:"stage"`
	Percent  float64      `json:"percent"`
	Revision uint64       `json:"revision,omitempty"`
	Reason   string       `json:"reason"`
	Gates    []GateResult `json:"gates,omitempty"`
	DryRun   bool         `json:"dry_run,omitempty"`
}

// Notifier receives every decision. Implementations must not block for long.
type Notifier interface {
	Notify(Decision)
}

// NotifyFunc adapts a function to Notifier.
type NotifyFunc func(Decision)

// Notify implements Notifier.
func (f NotifyFunc) Notify(d Decision) { f(d) }

// Multi fans a decision out to several notifiers.
func Multi(ns ...Notifier) Notifier {
	return NotifyFunc(func(d Decision) {
		for _, n := range ns {
			if n != nil {
				n.Notify(d)
			}
		}
	})
}

// LogNotifier logs every decision as a structured record.
func LogNotifier(l *slog.Logger) Notifier {
	return NotifyFunc(func(d Decision) {
		lvl := slog.LevelInfo
		if d.Action == ActionRollback || d.Action == ActionError || d.Action == ActionYield {
			lvl = slog.LevelWarn
		}
		args := []any{"action", string(d.Action), "stage", d.Stage, "percent", d.Percent, "revision", d.Revision, "reason", d.Reason, "dry_run", d.DryRun}
		for _, g := range d.Gates {
			args = append(args, "gate."+g.Name, string(g.Verdict)+": "+g.Detail)
		}
		l.Log(context.Background(), lvl, "rollout decision", args...)
	})
}

// KubeEvents posts a Kubernetes Event for every decision except holds (which
// would flood the event stream), attached to an object of your choice. Use
// InClusterKubeEvents inside a pod; the service account needs create on events.
type KubeEvents struct {
	API       string // https://kubernetes.default.svc
	Namespace string
	Token     string
	Client    *http.Client
	// Object is what the events are about, e.g. {Kind: "Deployment", Name: "backend-canary"}.
	Object ObjectRef
	Now    func() time.Time
}

// ObjectRef names the Kubernetes object events are attached to.
type ObjectRef struct{ Kind, Name, APIVersion string }

// InClusterKubeEvents builds a KubeEvents from the pod's service account.
func InClusterKubeEvents(obj ObjectRef) (*KubeEvents, error) {
	const dir = "/var/run/secrets/kubernetes.io/serviceaccount/"
	tok, err := os.ReadFile(dir + "token")
	if err != nil {
		return nil, err
	}
	ns, err := os.ReadFile(dir + "namespace")
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(dir + "ca.crt")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" {
		return nil, fmt.Errorf("not running in a cluster (KUBERNETES_SERVICE_HOST is empty)")
	}
	return &KubeEvents{
		API: "https://" + host + ":" + port, Namespace: strings.TrimSpace(string(ns)), Token: strings.TrimSpace(string(tok)), Object: obj,
		Client: &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}},
	}, nil
}

// Notify implements Notifier; errors are dropped (an event is not worth failing a rollout).
func (k *KubeEvents) Notify(d Decision) {
	if d.Action == ActionHold {
		return
	}
	now := time.Now
	if k.Now != nil {
		now = k.Now
	}
	typ := "Normal"
	if d.Action == ActionRollback || d.Action == ActionError || d.Action == ActionYield {
		typ = "Warning"
	}
	ts := now().UTC().Format(time.RFC3339)
	api := k.Object.APIVersion
	if api == "" {
		api = "apps/v1"
	}
	msg := fmt.Sprintf("stage %d (%.4g%%): %s", d.Stage, d.Percent, d.Reason)
	if d.DryRun {
		msg = "[dry run] " + msg
	}
	ev := map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata":       map[string]any{"generateName": "gocpp-rollout.", "namespace": k.Namespace},
		"involvedObject": map[string]any{"apiVersion": api, "kind": k.Object.Kind, "name": k.Object.Name, "namespace": k.Namespace},
		"reason":         "Rollout" + strings.ToUpper(string(d.Action)[:1]) + string(d.Action)[1:],
		"message":        msg, "type": typ, "firstTimestamp": ts, "lastTimestamp": ts, "count": 1,
		"source": map[string]any{"component": "gocpp-rollout"},
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(k.API, "/")+"/api/v1/namespaces/"+k.Namespace+"/events", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.Token)
	cl := k.Client
	if cl == nil {
		cl = http.DefaultClient
	}
	if resp, err := cl.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}
