package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/attrsource"
)

// attrSource builds the external attribute source from the flags (nil: none).
func attrSource(cfg config) gateway.AttrSource {
	switch {
	case cfg.AttrSourceFile != "":
		return &attrsource.File{Path: cfg.AttrSourceFile}
	case cfg.AttrSourceURL != "":
		h := &attrsource.HTTP{URL: cfg.AttrSourceURL}
		if cfg.AttrSourceToken != "" {
			h.Header = http.Header{"Authorization": {"Bearer " + cfg.AttrSourceToken}}
		}
		return h
	}
	return nil
}

type attrSourceMetrics struct{ lookups *prometheus.CounterVec }

func newAttrSourceMetrics(reg prometheus.Registerer) *attrSourceMetrics {
	m := &attrSourceMetrics{lookups: prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gocpp_gateway_attr_source_total", Help: "external attribute lookups by result (ok, timeout, error)"}, []string{"result"})}
	reg.MustRegister(m.lookups)
	return m
}

func (m *attrSourceMetrics) result(r string) { m.lookups.WithLabelValues(r).Inc() }
