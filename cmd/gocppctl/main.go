// Command gocppctl is the operator CLI of a split CSMS: staged releases of the
// processing tier (route) and of the connection tier (gateway). It talks to the
// session registry (Redis); it never touches a charger. See docs/staged-releases.md.
//
//	gocppctl route validate FILE
//	gocppctl route show [--history]
//	gocppctl route apply FILE [--expect-rev N] [--wait 30s] [--force]
//	gocppctl route rollback [--to REV]
//	gocppctl route status
//	gocppctl gateway status|shed|cap|rebalance
//	gocppctl rollout validate|run
//
// Connection flags (before the subcommand's own): --redis-addr (env
// GOCPPCTL_REDIS_ADDR), --prefix (the registry key prefix, default gocpp:sess),
// Redis TLS (--redis-tls, --redis-ca, --redis-cert/--redis-key,
// --redis-server-name). Secrets are never flags (they show up in ps): the Redis
// password comes from GOCPPCTL_REDIS_PASSWORD or --redis-password-file, the
// gateways' admin token from GOCPPCTL_ADMIN_TOKEN or --admin-token-file.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
)

// Exit codes: 1 error, 2 usage, 3 written but not converged.
const (
	exitErr         = 1
	exitUsage       = 2
	exitNotConverge = 3
)

type conn struct {
	addr, prefix string
	passFile     string
	tls          bool
	ca           string
	cert, key    string
	serverName   string
}

func (c *conn) flags(fs *flag.FlagSet) {
	fs.StringVar(&c.addr, "redis-addr", os.Getenv("GOCPPCTL_REDIS_ADDR"), "Redis address of the session registry")
	fs.StringVar(&c.passFile, "redis-password-file", os.Getenv("GOCPPCTL_REDIS_PASSWORD_FILE"), "file holding the Redis password (or set GOCPPCTL_REDIS_PASSWORD)")
	fs.BoolVar(&c.tls, "redis-tls", os.Getenv("GOCPPCTL_REDIS_TLS") == "true", "connect to Redis over TLS")
	fs.StringVar(&c.ca, "redis-ca", os.Getenv("GOCPPCTL_REDIS_CA"), "CA file that verifies the Redis server (implies --redis-tls)")
	fs.StringVar(&c.cert, "redis-cert", os.Getenv("GOCPPCTL_REDIS_CERT"), "client certificate for Redis mTLS")
	fs.StringVar(&c.key, "redis-key", os.Getenv("GOCPPCTL_REDIS_KEY"), "client key for Redis mTLS")
	fs.StringVar(&c.serverName, "redis-server-name", os.Getenv("GOCPPCTL_REDIS_SERVER_NAME"), "server name to verify (default: the host of --redis-addr)")
	fs.StringVar(&c.prefix, "prefix", "gocpp:sess", "registry key prefix")
}

// secretFromFile returns the trimmed content of file, or the environment
// variable envName when file is empty.
func secretFromFile(file, envName string) (string, error) {
	if file == "" {
		return os.Getenv(envName), nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// tlsConfig builds a client TLS configuration; ca, cert and key are file paths
// and may be empty.
func tlsConfig(ca, cert, key, serverName string) (*tls.Config, error) {
	tc := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if ca != "" {
		pem, err := os.ReadFile(ca)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s: no certificates", ca)
		}
		tc.RootCAs = pool
	}
	if (cert == "") != (key == "") {
		return nil, errors.New("certificate and key must be given together")
	}
	if cert != "" {
		kp, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		tc.Certificates = []tls.Certificate{kp}
	}
	return tc, nil
}

func (c *conn) redisOptions() (*redis.Options, error) {
	pass, err := secretFromFile(c.passFile, "GOCPPCTL_REDIS_PASSWORD")
	if err != nil {
		return nil, err
	}
	o := &redis.Options{Addr: c.addr, Password: pass}
	if c.tls || c.ca != "" {
		sni := c.serverName
		if sni == "" {
			if h, _, err := net.SplitHostPort(c.addr); err == nil {
				sni = h
			}
		}
		if o.TLSConfig, err = tlsConfig(c.ca, c.cert, c.key, sni); err != nil {
			return nil, err
		}
	}
	return o, nil
}

func (c *conn) registry() (*registryredis.Registry, func(), error) {
	if c.addr == "" {
		return nil, nil, errors.New("--redis-addr (or GOCPPCTL_REDIS_ADDR) is required")
	}
	opts, err := c.redisOptions()
	if err != nil {
		return nil, nil, err
	}
	rdb := redis.NewClient(opts)
	return registryredis.New(rdb, registryredis.WithKeyPrefix(c.prefix)), func() { _ = rdb.Close() }, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: gocppctl <command> [flags]

commands:
  route validate FILE        check a routing policy offline
  route show [--history]     print the policy in force (or the stored history)
  route apply FILE           store FILE as the next revision and wait for the gateways
  route rollback [--to REV]  restore the previous (or a given) revision, ignoring holds
  route status               revision per gateway; exit 3 if they differ
  gateway status             sessions, state and admin address per gateway
  gateway shed               close sessions of one gateway at a paced rate (partial drain)
  gateway cap                set or remove a gateway's soft session cap (canary share)
  gateway rebalance          shed the excess from gateways above their fair share
  rollout validate FILE      check an automatic rollout plan
  rollout run FILE           run the plan: stages, gates, auto-rollback (--dry-run to only report)
  version

run "gocppctl <command> -h" for flags.
`)
}

func run(ctx context.Context, args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		usage(errw)
		return exitUsage
	}
	var err error
	switch args[0] {
	case "route":
		err = routeCmd(ctx, args[1:], out)
	case "rollout":
		err = rolloutCmd(ctx, args[1:], out)
	case "gateway":
		err = gatewayCmd(ctx, args[1:], out)
	case "version":
		fmt.Fprintln(out, version)
	case "help", "-h", "--help":
		usage(out)
	default:
		usage(errw)
		return exitUsage
	}
	var ex *exitError
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.As(err, &ex):
		fmt.Fprintln(errw, "gocppctl:", ex.err)
		return ex.code
	default:
		fmt.Fprintln(errw, "gocppctl:", err)
		return exitErr
	}
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// version is set at build time.
var version = "dev"
