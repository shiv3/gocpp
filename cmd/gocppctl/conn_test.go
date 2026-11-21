package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnSecretsComeFromFileOrEnvNotFlags(t *testing.T) {
	var c conn
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	c.flags(fs)
	require.Error(t, fs.Parse([]string{"--redis-password", "x"}), "the password is not a flag")

	t.Setenv("GOCPPCTL_REDIS_PASSWORD", "fromenv")
	o, err := (&conn{addr: "h:6379"}).redisOptions()
	require.NoError(t, err)
	require.Equal(t, "fromenv", o.Password)
	require.Nil(t, o.TLSConfig)

	f := filepath.Join(t.TempDir(), "pw")
	require.NoError(t, os.WriteFile(f, []byte("fromfile\n"), 0o600))
	o, err = (&conn{addr: "redis.example:6380", passFile: f, tls: true}).redisOptions()
	require.NoError(t, err)
	require.Equal(t, "fromfile", o.Password)
	require.NotNil(t, o.TLSConfig)
	require.Equal(t, "redis.example", o.TLSConfig.ServerName)

	_, err = (&conn{addr: "h:1", ca: filepath.Join(t.TempDir(), "missing")}).redisOptions()
	require.Error(t, err)
}
