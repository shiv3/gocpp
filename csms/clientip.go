package csms

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP returns the address of the charge point behind remoteAddr (the
// peer address of the TCP connection, "host:port" or a bare host) given the
// headers of its upgrade request.
//
// Without trusted proxies, or when the peer itself is not one of them, the
// headers are ignored (anyone can send them) and the result is the host of
// remoteAddr. Otherwise the X-Forwarded-For chain is walked from the right,
// skipping hops that are trusted proxies; the first hop that is not trusted is
// the client. If every hop is trusted the leftmost one is returned. An entry
// that is not an IP address ends the walk at the hop to its right. Without
// X-Forwarded-For a valid X-Real-IP is used.
//
// The result is an IP address without a port, or the host part of remoteAddr
// unchanged when that is not an IP address.
func ClientIP(remoteAddr string, h http.Header, trusted []netip.Prefix) string {
	peerStr := hostOf(remoteAddr)
	peer, err := netip.ParseAddr(peerStr)
	if err != nil || len(trusted) == 0 || !inPrefixes(trusted, peer.Unmap()) {
		return peerStr
	}
	if xff := h.Values("X-Forwarded-For"); len(xff) > 0 {
		var hops []string
		for _, v := range xff {
			hops = append(hops, strings.Split(v, ",")...)
		}
		client := peerStr
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				return client
			}
			client = a.Unmap().String()
			if !inPrefixes(trusted, a.Unmap()) {
				return client
			}
		}
		return client
	}
	if a, err := netip.ParseAddr(strings.TrimSpace(h.Get("X-Real-IP"))); err == nil {
		return a.Unmap().String()
	}
	return peerStr
}

func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return strings.Trim(addr, "[]")
}

func inPrefixes(ps []netip.Prefix, a netip.Addr) bool {
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP returns the IP address of the charge point: the host of RemoteAddr,
// or, when the server was given WithTrustedProxies and the peer is one of them,
// the client address taken from X-Forwarded-For / X-Real-IP (see the package
// function ClientIP). It is empty for a handle of a charge point held by
// another instance.
func (c *Conn) ClientIP() string {
	var trusted []netip.Prefix
	if c.srv != nil {
		trusted = c.srv.cfg.trustedProxies
	}
	if c.inner == nil {
		if c.gw != nil && c.gw.info() != nil {
			return ClientIP(c.gw.info().RemoteAddr, c.gw.info().RequestHeader, trusted)
		}
		return ""
	}
	return c.inner.ClientIP()
}
