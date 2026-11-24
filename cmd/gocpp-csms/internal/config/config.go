// Package config holds the serve-mode configuration of gocpp-csms: command
// line flags (stdlib flag) whose defaults come from GOCPP_CSMS_* environment
// variables.
//
// Extensions declare their own flags through a Binder, which applies the same
// naming rule: flag "--foo-bar" has the environment default GOCPP_CSMS_FOO_BAR.
package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// EnvPrefix is prepended to every flag's environment variable name.
const EnvPrefix = "GOCPP_CSMS_"

// Env looks up an environment variable (os.Getenv in production).
type Env func(string) string

// EnvName returns the environment variable that holds the default of flag name.
func EnvName(name string) string {
	return EnvPrefix + strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}

// Binder defines flags on a FlagSet with environment defaults. An environment
// value that does not parse is reported by Err after the flags are defined.
type Binder struct {
	fs   *flag.FlagSet
	env  Env
	errs []error
}

// NewBinder returns a Binder for fs reading defaults from env.
func NewBinder(fs *flag.FlagSet, env Env) *Binder {
	if env == nil {
		env = func(string) string { return "" }
	}
	return &Binder{fs: fs, env: env}
}

// FlagSet returns the underlying flag set.
func (b *Binder) FlagSet() *flag.FlagSet { return b.fs }

// Err returns the environment values that could not be parsed.
func (b *Binder) Err() error { return errors.Join(b.errs...) }

func (b *Binder) lookup(name string) (string, string, bool) {
	key := EnvName(name)
	v := b.env(key)
	return key, v, v != ""
}

func (b *Binder) usage(name, usage string) string {
	return fmt.Sprintf("%s (env %s)", usage, EnvName(name))
}

// String defines a string flag.
func (b *Binder) String(p *string, name, def, usage string) {
	if _, v, ok := b.lookup(name); ok {
		def = v
	}
	b.fs.StringVar(p, name, def, b.usage(name, usage))
}

// Bool defines a bool flag.
func (b *Binder) Bool(p *bool, name string, def bool, usage string) {
	if key, v, ok := b.lookup(name); ok {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("%s=%q: %w", key, v, err))
		} else {
			def = parsed
		}
	}
	b.fs.BoolVar(p, name, def, b.usage(name, usage))
}

// Int defines an int flag.
func (b *Binder) Int(p *int, name string, def int, usage string) {
	if key, v, ok := b.lookup(name); ok {
		parsed, err := strconv.Atoi(v)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("%s=%q: %w", key, v, err))
		} else {
			def = parsed
		}
	}
	b.fs.IntVar(p, name, def, b.usage(name, usage))
}

// Var defines a flag of a custom type; def is parsed with value.Set.
func (b *Binder) Var(value flag.Value, name, def, usage string) {
	if key, v, ok := b.lookup(name); ok {
		if err := value.Set(v); err != nil {
			b.errs = append(b.errs, fmt.Errorf("%s=%q: %w", key, v, err))
			_ = value.Set(def)
		}
	} else if err := value.Set(def); err != nil {
		b.errs = append(b.errs, fmt.Errorf("default of --%s: %w", name, err))
	}
	b.fs.Var(value, name, b.usage(name, usage))
}

// Duration defines a time.Duration flag.
func (b *Binder) Duration(p *time.Duration, name string, def time.Duration, usage string) {
	if key, v, ok := b.lookup(name); ok {
		parsed, err := time.ParseDuration(v)
		if err != nil {
			b.errs = append(b.errs, fmt.Errorf("%s=%q: %w", key, v, err))
		} else {
			def = parsed
		}
	}
	b.fs.DurationVar(p, name, def, b.usage(name, usage))
}

// Config is the serve-mode configuration.
type Config struct {
	// OCPPAddr is the listen address of the OCPP WebSocket endpoint.
	OCPPAddr string
	// ControlAddr is the listen address of the HTTP control API, health,
	// readiness and metrics.
	ControlAddr string
	// PathPrefix is the URL path a charge point dials under; the charge
	// point id is the last path segment (PathPrefix + [anything/] + cpId).
	PathPrefix string
	// Versions are the OCPP versions accepted (subprotocol negotiation).
	Versions []string

	TLSCert     string
	TLSKey      string
	TLSClientCA string
	// MTLSMatchCPID requires the client certificate's common name to equal
	// the charge point id (needs TLSClientCA).
	MTLSMatchCPID bool

	// BasicAuthFile lists "cpId:password" or "cpId:<bcrypt hash>" lines.
	BasicAuthFile string

	LogFormat string
	LogLevel  string

	MaxConnections int
	// InboundConcurrency caps inbound handlers across all connections
	// (0 = unlimited). 1 makes the recorder's seq the receive order.
	InboundConcurrency int
	DrainTimeout       time.Duration
	CommandTimeout     time.Duration
	// BootInterval is the heartbeat interval sent in BootNotification answers.
	BootInterval int
	// Schema is the schema validation mode: strict, tolerant, lenient or off.
	Schema string

	// Record writes the NDJSON frame log on stdout.
	Record bool
	// Announce writes the E2E_CSMS_PORTS sentinel as the first stdout line.
	Announce bool
}

// Versions accepted by serve mode.
var knownVersions = map[string]string{"1.6": "ocpp1.6", "2.0.1": "ocpp2.0.1", "2.1": "ocpp2.1"}

// Subprotocol returns the WebSocket subprotocol of an OCPP version.
func Subprotocol(version string) (string, bool) {
	s, ok := knownVersions[version]
	return s, ok
}

// Bind defines the serve-mode flags on b and returns the Config they fill
// once the flag set is parsed. Call Finish after parsing.
func Bind(b *Binder) *Config {
	c := &Config{}
	b.String(&c.OCPPAddr, "ocpp-addr", ":8080", "OCPP WebSocket listen address")
	b.String(&c.ControlAddr, "control-addr", "127.0.0.1:8081", "HTTP control API, /healthz, /readyz and /metrics listen address")
	b.String(&c.PathPrefix, "path-prefix", "/ocpp/", "URL path prefix charge points dial under; the charge point id is the last path segment")
	b.Var(versionsValue{&c.Versions}, "versions", "2.1,2.0.1,1.6", "comma-separated OCPP versions to accept, in order of preference")
	b.String(&c.TLSCert, "tls-cert", "", "TLS certificate file (PEM); enables wss://")
	b.String(&c.TLSKey, "tls-key", "", "TLS private key file (PEM)")
	b.String(&c.TLSClientCA, "tls-client-ca", "", "CA bundle (PEM) for client certificates; requires and verifies one (mTLS)")
	b.Bool(&c.MTLSMatchCPID, "mtls-match-cpid", false, "require the client certificate common name to equal the charge point id")
	b.String(&c.BasicAuthFile, "basic-auth-file", "", `HTTP Basic Auth credentials, one "cpId:password" or "cpId:<bcrypt hash>" per line`)
	b.String(&c.LogFormat, "log-format", "json", "log format: json or text (stderr)")
	b.String(&c.LogLevel, "log-level", "info", "log level: debug, info, warn or error")
	b.Int(&c.MaxConnections, "max-connections", 0, "maximum simultaneous charge point connections (0 = unlimited)")
	b.Int(&c.InboundConcurrency, "inbound-concurrency", 0, "maximum inbound handlers running at once across all connections (0 = unlimited; 1 = recorder seq in receive order)")
	b.Duration(&c.DrainTimeout, "drain-timeout", 30*time.Second, "on SIGTERM, time to close charge point connections before stopping")
	b.Duration(&c.CommandTimeout, "command-timeout", 30*time.Second, "timeout of a CSMS-initiated command")
	b.Int(&c.BootInterval, "boot-interval", 300, "heartbeat interval (seconds) sent in BootNotification answers")
	b.String(&c.Schema, "schema", "strict", "schema validation of charge point messages: strict, tolerant, lenient or off")
	b.Bool(&c.Record, "record", false, "write one NDJSON line per received charge point CALL on stdout")
	b.Bool(&c.Announce, "announce", false, `write "E2E_CSMS_PORTS {\"ocpp\":N,\"http\":N}" as the first stdout line`)
	return c
}

// Validate checks the parsed configuration.
func (c *Config) Validate() error {
	var errs []error
	if c.OCPPAddr == "" {
		errs = append(errs, errors.New("--ocpp-addr must not be empty"))
	}
	if c.ControlAddr == "" {
		errs = append(errs, errors.New("--control-addr must not be empty"))
	}
	if !strings.HasPrefix(c.PathPrefix, "/") {
		errs = append(errs, fmt.Errorf("--path-prefix %q must start with /", c.PathPrefix))
	}
	if len(c.Versions) == 0 {
		errs = append(errs, errors.New("--versions must name at least one of 1.6, 2.0.1, 2.1"))
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		errs = append(errs, errors.New("--tls-cert and --tls-key go together"))
	}
	if c.TLSClientCA != "" && c.TLSCert == "" {
		errs = append(errs, errors.New("--tls-client-ca needs --tls-cert and --tls-key"))
	}
	if c.MTLSMatchCPID && c.TLSClientCA == "" {
		errs = append(errs, errors.New("--mtls-match-cpid needs --tls-client-ca"))
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("--log-format %q: expected json or text", c.LogFormat))
	}
	if _, err := ParseLevel(c.LogLevel); err != nil {
		errs = append(errs, err)
	}
	switch c.Schema {
	case "strict", "tolerant", "lenient", "off":
	default:
		errs = append(errs, fmt.Errorf("--schema %q: expected strict, tolerant, lenient or off", c.Schema))
	}
	if c.MaxConnections < 0 {
		errs = append(errs, errors.New("--max-connections must not be negative"))
	}
	if c.InboundConcurrency < 0 {
		errs = append(errs, errors.New("--inbound-concurrency must not be negative"))
	}
	if c.DrainTimeout < 0 {
		errs = append(errs, errors.New("--drain-timeout must not be negative"))
	}
	if c.CommandTimeout <= 0 {
		errs = append(errs, errors.New("--command-timeout must be positive"))
	}
	if c.BootInterval < 1 {
		errs = append(errs, errors.New("--boot-interval must be at least 1"))
	}
	return errors.Join(errs...)
}

// Subprotocols returns the WebSocket subprotocols of the configured versions.
func (c *Config) Subprotocols() []string {
	out := make([]string, 0, len(c.Versions))
	for _, v := range c.Versions {
		out = append(out, knownVersions[v])
	}
	return out
}

// ParseLevel parses a log level name.
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("--log-level %q: expected debug, info, warn or error", s)
	}
	return l, nil
}

// versionsValue is the --versions flag: a comma-separated list of versions.
type versionsValue struct{ p *[]string }

func (v versionsValue) String() string {
	if v.p == nil {
		return ""
	}
	return strings.Join(*v.p, ",")
}

func (v versionsValue) Set(s string) error {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := knownVersions[part]; !ok {
			return fmt.Errorf("unknown OCPP version %q; expected 1.6, 2.0.1 or 2.1", part)
		}
		if !seen[part] {
			seen[part] = true
			out = append(out, part)
		}
	}
	*v.p = out
	return nil
}
