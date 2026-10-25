// Package clientip determines the address of the client behind a request,
// honoring X-Forwarded-For only when it was set by a trusted reverse proxy.
// Returned addresses are unmapped (IPv4-mapped IPv6 becomes IPv4) and carry
// no zone, so they are stable keys, e.g. for rate limiting.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrusted parses a comma-separated list of CIDR prefixes or single IP
// addresses, such as "10.0.0.0/8, 192.0.2.1". Whitespace around items and
// empty items are ignored, so an empty string yields nil. Host bits of a
// prefix are masked off and IPv4-mapped IPv6 entries are stored as IPv4.
func ParseTrusted(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix

	for item := range strings.SplitSeq(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		p, err := parsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("clientip: invalid trusted proxy %q: %w", item, err)
		}

		out = append(out, p)
	}

	return out, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if !strings.Contains(s, "/") {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, err
		}

		a = normalize(a)

		return netip.PrefixFrom(a, a.BitLen()), nil
	}

	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, err
	}

	if a := p.Addr(); a.Is4In6() {
		const mappedBits = 96 // the ::ffff:0:0/96 prefix of IPv4-mapped addresses
		if p.Bits() < mappedBits {
			return netip.Prefix{}, fmt.Errorf("IPv4-mapped prefix shorter than /%d", mappedBits)
		}

		p = netip.PrefixFrom(a.Unmap(), p.Bits()-mappedBits)
	}

	return p.Masked(), nil
}

// From returns the client IP of r. It is the host of r.RemoteAddr unless
// that address is within trusted; then X-Forwarded-For (all header values,
// comma-separated) is read from the right, skipping trusted addresses, and
// the first untrusted one is the client. Entries left of it were written by
// the client and are never believed. A malformed entry stops the walk and
// the last trusted hop seen is returned. Without trusted prefixes
// X-Forwarded-For is ignored entirely. An unparsable RemoteAddr yields the
// zero netip.Addr.
func From(r *http.Request, trusted []netip.Prefix) netip.Addr {
	return FromAddr(r.RemoteAddr, r.Header, trusted)
}

// FromAddr is From for a request given by its remote address and headers,
// for callers that have no *http.Request at hand.
func FromAddr(remote string, header http.Header, trusted []netip.Prefix) netip.Addr {
	peer := remoteAddr(remote)
	if !peer.IsValid() || !isTrusted(peer, trusted) {
		return peer
	}

	hops := forwardedFor(header)

	last := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return last
		}

		a = normalize(a)
		if !isTrusted(a, trusted) {
			return a
		}

		last = a
	}

	return last
}

// PeerTrusted reports whether the direct peer of r (its RemoteAddr) is one
// of the trusted proxies, so that headers the proxy sets, such as a request
// ID, may be believed. Without trusted prefixes it is always false.
func PeerTrusted(r *http.Request, trusted []netip.Prefix) bool {
	peer := remoteAddr(r.RemoteAddr)

	return peer.IsValid() && isTrusted(peer, trusted)
}

// remoteAddr parses the host of an http.Request.RemoteAddr ("ip:port",
// "[ipv6%zone]:port" or, leniently, a bare IP).
func remoteAddr(s string) netip.Addr {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		host = s
	}

	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}

	return normalize(a)
}

// forwardedFor returns the X-Forwarded-For entries of all header lines in
// order, left to right.
func forwardedFor(h http.Header) []string {
	var hops []string
	for _, v := range h.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(v, ",")...)
	}

	return hops
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}

	return false
}

func normalize(a netip.Addr) netip.Addr {
	return a.Unmap().WithZone("")
}
