package attrsource_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/gateway/attrsource"
	"github.com/stretchr/testify/require"
)

func TestFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "labels.yaml")
	require.NoError(t, os.WriteFile(p, []byte("CP_1: {site: berlin, label.canary: 1}\n\"*\": {fleet: eu}\n"), 0o600))
	f := &attrsource.File{Path: p}
	got, err := f.Attrs(context.Background(), "CP_1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"site": "berlin", "label.canary": "1", "fleet": "eu"}, got)
	got, err = f.Attrs(context.Background(), "CP_9")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"fleet": "eu"}, got)

	// A bad file keeps the previous contents; a changed one is picked up.
	require.NoError(t, os.WriteFile(p, []byte("CP_1: [not a map"), 0o600))
	time.Sleep(1100 * time.Millisecond)
	got, err = f.Attrs(context.Background(), "CP_1")
	require.NoError(t, err)
	require.Equal(t, "berlin", got["site"])
	require.NoError(t, os.WriteFile(p, []byte("CP_1: {site: paris, extra: yes-please}\n"), 0o600))
	time.Sleep(1100 * time.Millisecond)
	got, _ = f.Attrs(context.Background(), "CP_1")
	require.Equal(t, map[string]string{"site": "paris", "extra": "yes-please"}, got)

	_, err = (&attrsource.File{Path: filepath.Join(t.TempDir(), "missing")}).Attrs(context.Background(), "x")
	require.Error(t, err)
}

func TestHTTP(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/labels/CP_1":
			_, _ = w.Write([]byte(`{"site":"berlin","tier":2,"beta":true,"skip":null}`))
		case "/labels/CP_bad":
			_, _ = w.Write([]byte(`{"nested":{"a":1}}`))
		case "/labels/CP_err":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := &attrsource.HTTP{URL: srv.URL + "/labels/{cpid}", Header: http.Header{"Authorization": {"Bearer t"}}, TTL: time.Hour}
	ctx := context.Background()
	got, err := h.Attrs(ctx, "CP_1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"site": "berlin", "tier": "2", "beta": "true"}, got)
	_, _ = h.Attrs(ctx, "CP_1")
	require.EqualValues(t, 1, hits.Load(), "cached")
	got, err = h.Attrs(ctx, "CP_unknown")
	require.NoError(t, err)
	require.Empty(t, got, "404 = no attributes")
	_, err = h.Attrs(ctx, "CP_bad")
	require.Error(t, err)
	_, err = h.Attrs(ctx, "CP_err")
	require.Error(t, err)

	// appended id, no cache
	h2 := &attrsource.HTTP{URL: srv.URL + "/labels", Header: http.Header{"Authorization": {"Bearer t"}}, TTL: -1}
	_, _ = h2.Attrs(ctx, "CP_1")
	before := hits.Load()
	_, _ = h2.Attrs(ctx, "CP_1")
	require.Equal(t, before+1, hits.Load())
}
