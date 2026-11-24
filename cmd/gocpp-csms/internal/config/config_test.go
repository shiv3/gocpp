package config

import (
	"flag"
	"io"
	"testing"
	"time"
)

func parse(t *testing.T, env map[string]string, args ...string) (*Config, error) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	b := NewBinder(fs, func(k string) string { return env[k] })
	c := Bind(b)
	if err := b.Err(); err != nil {
		return nil, err
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return c, c.Validate()
}

func TestDefaultsEnvAndFlags(t *testing.T) {
	c, err := parse(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.OCPPAddr != ":8080" || c.ControlAddr != "127.0.0.1:8081" || c.PathPrefix != "/ocpp/" || len(c.Versions) != 3 || c.Schema != "strict" || c.BootInterval != 300 {
		t.Fatalf("defaults %+v", c)
	}
	c, err = parse(t, map[string]string{
		"GOCPP_CSMS_OCPP_ADDR":     ":9000",
		"GOCPP_CSMS_VERSIONS":      "1.6",
		"GOCPP_CSMS_RECORD":        "true",
		"GOCPP_CSMS_DRAIN_TIMEOUT": "2s",
	}, "--versions", "2.0.1,1.6")
	if err != nil {
		t.Fatal(err)
	}
	if c.OCPPAddr != ":9000" || !c.Record || c.DrainTimeout != 2*time.Second {
		t.Fatalf("env %+v", c)
	}
	if got := c.Subprotocols(); len(got) != 2 || got[0] != "ocpp2.0.1" || got[1] != "ocpp1.6" {
		t.Fatalf("flag beats env: %v", got)
	}
}

func TestInvalid(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		args []string
	}{
		{map[string]string{"GOCPP_CSMS_MAX_CONNECTIONS": "many"}, nil},
		{nil, []string{"--versions", "1.5"}},
		{nil, []string{"--tls-cert", "a.pem"}},
		{nil, []string{"--mtls-match-cpid"}},
		{nil, []string{"--log-format", "xml"}},
		{nil, []string{"--schema", "loose"}},
		{nil, []string{"--path-prefix", "ocpp"}},
	} {
		if _, err := parse(t, tc.env, tc.args...); err == nil {
			t.Errorf("%v %v: no error", tc.env, tc.args)
		}
	}
}
