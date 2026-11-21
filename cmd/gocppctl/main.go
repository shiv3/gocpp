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
// GOCPPCTL_REDIS_ADDR), --redis-password (GOCPPCTL_REDIS_PASSWORD), --prefix
// (the registry key prefix, default gocpp:sess).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
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
	addr, password, prefix string
}

func (c *conn) flags(fs *flag.FlagSet) {
	fs.StringVar(&c.addr, "redis-addr", os.Getenv("GOCPPCTL_REDIS_ADDR"), "Redis address of the session registry")
	fs.StringVar(&c.password, "redis-password", os.Getenv("GOCPPCTL_REDIS_PASSWORD"), "Redis password")
	fs.StringVar(&c.prefix, "prefix", "gocpp:sess", "registry key prefix")
}

func (c *conn) registry() (*registryredis.Registry, func(), error) {
	if c.addr == "" {
		return nil, nil, errors.New("--redis-addr (or GOCPPCTL_REDIS_ADDR) is required")
	}
	rdb := redis.NewClient(&redis.Options{Addr: c.addr, Password: c.password})
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
