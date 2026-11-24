package server

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
)

// Main runs "gocpp-csms serve" with args (after the subcommand) and returns
// the exit status: 0 after a signal-initiated shutdown, 1 on a runtime error,
// 2 on a usage error.
func Main(args []string, env config.Env, stdout, stderr io.Writer, exts ...Extension) int {
	fs := flag.NewFlagSet("gocpp-csms serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	b := config.NewBinder(fs, env)
	cfg := config.Bind(b)
	for _, ext := range exts {
		ext.Flags(b)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if err := b.Err(); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	if fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if err := cfg.Validate(); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	logger := NewLogger(cfg, stderr)

	s, err := New(cfg, logger, stdout, exts...)
	if err != nil {
		logger.Error("configure", "error", err)
		return 1
	}
	if err := s.Start(); err != nil {
		logger.Error("start", "error", err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		return 1
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	exit := 0
	select {
	case sig := <-sigCh:
		logger.Info("shutdown requested", "signal", sig.String(), "drain_timeout", cfg.DrainTimeout.String())
	case err := <-s.Err():
		logger.Error("server stopped", "error", err)
		exit = 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DrainTimeout+15*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		logger.Error("shutdown", "error", err)
		exit = 1
	}
	return exit
}

// NewLogger builds the slog logger of cfg writing to w.
func NewLogger(cfg *config.Config, w io.Writer) *slog.Logger {
	level, _ := config.ParseLevel(cfg.LogLevel)
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}
