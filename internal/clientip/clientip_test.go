package clientip_test

import (
	"net/http"
	"net/netip"
	"slices"
	"testing"

	"github.com/cwbudde/studio-schedule/internal/clientip"
)

func TestParseTrusted(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"10.0.0.0/8", []string{"10.0.0.0/8"}},
		{
			" 10.0.0.0/8 ,192.0.2.1,\t2001:db8::/32 ",
			[]string{"10.0.0.0/8", "192.0.2.1/32", "2001:db8::/32"},
		},
		{"2001:db8::1", []string{"2001:db8::1/128"}},
		// Host bits are masked off.
		{"10.1.2.3/8", []string{"10.0.0.0/8"}},
		// IPv4-mapped IPv6 is stored as IPv4, so it matches unmapped clients.
		{"::ffff:10.0.0.1", []string{"10.0.0.1/32"}},
		{"::ffff:10.0.0.0/104", []string{"10.0.0.0/8"}},
		// Zones carry no meaning for matching.
		{"fe80::1%eth0", []string{"fe80::1/128"}},
		// Empty items from a stray comma are ignored.
		{"10.0.0.0/8,", []string{"10.0.0.0/8"}},
	} {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()

			got, err := clientip.ParseTrusted(tc.in)
			if err != nil {
				t.Fatalf("ParseTrusted(%q): %v", tc.in, err)
			}

			var gotStr []string
			for _, p := range got {
				gotStr = append(gotStr, p.String())
			}

			if !slices.Equal(gotStr, tc.want) {
				t.Fatalf("ParseTrusted(%q) = %v, want %v", tc.in, gotStr, tc.want)
			}
		})
	}
}

func TestParseTrustedErrors(t *testing.T) {
	t.Parallel()

	for _, in := range []string{
		"proxy.example.org",
		"10.0.0.0/33",
		"10.0.0.1:8080",
		"10.0.0.0/8, nope",
		"::ffff:10.0.0.0/64",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()

			if got, err := clientip.ParseTrusted(in); err == nil {
				t.Fatalf("ParseTrusted(%q) = %v, want an error", in, got)
			}
		})
	}
}

func TestFrom(t *testing.T) {
	t.Parallel()

	proxies := mustParseTrusted(t, "10.0.0.0/8, 127.0.0.1, 2001:db8:ffff::/48")

	for _, tc := range []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{
			name:   "direct client",
			remote: "203.0.113.7:51234",
			want:   "203.0.113.7",
		},
		{
			name:   "spoofed header without trusted proxies",
			remote: "203.0.113.7:51234",
			xff:    []string{"198.51.100.1"},
			want:   "203.0.113.7",
		},
		{
			name:    "spoofed header from an untrusted peer",
			remote:  "203.0.113.7:51234",
			xff:     []string{"198.51.100.1"},
			trusted: proxies,
			want:    "203.0.113.7",
		},
		{
			name:    "behind a trusted proxy",
			remote:  "10.0.0.1:443",
			xff:     []string{"198.51.100.9"},
			trusted: proxies,
			want:    "198.51.100.9",
		},
		{
			name:    "forged left-most entry behind a trusted proxy",
			remote:  "10.0.0.1:443",
			xff:     []string{"6.6.6.6, 198.51.100.9"},
			trusted: proxies,
			want:    "198.51.100.9",
		},
		{
			name:    "chain of trusted proxies",
			remote:  "10.0.0.1:443",
			xff:     []string{"6.6.6.6, 198.51.100.9, 10.0.0.3,10.0.0.2"},
			trusted: proxies,
			want:    "198.51.100.9",
		},
		{
			name:    "several header lines are read as one list",
			remote:  "10.0.0.1:443",
			xff:     []string{"6.6.6.6, 198.51.100.9", "10.0.0.2"},
			trusted: proxies,
			want:    "198.51.100.9",
		},
		{
			name:    "trusted proxy without header",
			remote:  "10.0.0.1:443",
			trusted: proxies,
			want:    "10.0.0.1",
		},
		{
			name:    "every hop trusted",
			remote:  "10.0.0.1:443",
			xff:     []string{"10.0.0.3, 10.0.0.2"},
			trusted: proxies,
			want:    "10.0.0.3",
		},
		{
			name:    "malformed entry stops the walk",
			remote:  "10.0.0.1:443",
			xff:     []string{"198.51.100.9, unknown, 10.0.0.2"},
			trusted: proxies,
			want:    "10.0.0.2",
		},
		{
			name:    "malformed right-most entry",
			remote:  "10.0.0.1:443",
			xff:     []string{"198.51.100.9, 198.51.100.10:1234"},
			trusted: proxies,
			want:    "10.0.0.1",
		},
		{
			name:    "empty header",
			remote:  "10.0.0.1:443",
			xff:     []string{""},
			trusted: proxies,
			want:    "10.0.0.1",
		},
		{
			name:   "IPv6 in brackets",
			remote: "[2001:db8::1]:443",
			want:   "2001:db8::1",
		},
		{
			name:   "IPv6 with zone",
			remote: "[fe80::1%en0]:443",
			want:   "fe80::1",
		},
		{
			name:   "IPv4-mapped IPv6 peer",
			remote: "[::ffff:192.0.2.1]:80",
			want:   "192.0.2.1",
		},
		{
			name:    "IPv4-mapped trusted proxy and client",
			remote:  "[::ffff:127.0.0.1]:8080",
			xff:     []string{"::ffff:198.51.100.9"},
			trusted: proxies,
			want:    "198.51.100.9",
		},
		{
			name:    "IPv6 proxy and client",
			remote:  "[2001:db8:ffff::1]:443",
			xff:     []string{"2001:db8:1::42"},
			trusted: proxies,
			want:    "2001:db8:1::42",
		},
		{
			name:   "WASM bridge",
			remote: "127.0.0.1:0",
			want:   "127.0.0.1",
		},
		{
			name:   "WASM bridge ignores client headers",
			remote: "127.0.0.1:0",
			xff:    []string{"198.51.100.9"},
			want:   "127.0.0.1",
		},
		{
			name:   "address without port",
			remote: "192.0.2.1",
			want:   "192.0.2.1",
		},
		{
			name:   "host name",
			remote: "proxy.example.org:443",
			want:   "invalid IP",
		},
		{
			name:   "garbage",
			remote: "not an address",
			want:   "invalid IP",
		},
		{
			name:   "empty",
			remote: "",
			want:   "invalid IP",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, err := http.NewRequest(http.MethodGet, "/api/v1/login", nil)
			if err != nil {
				t.Fatal(err)
			}

			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}

			if got := clientip.From(r, tc.trusted); got.String() != tc.want {
				t.Fatalf("From(RemoteAddr %q, XFF %q) = %s, want %s", tc.remote, tc.xff, got, tc.want)
			}
		})
	}
}

func mustParseTrusted(t *testing.T, s string) []netip.Prefix {
	t.Helper()

	p, err := clientip.ParseTrusted(s)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestPeerTrusted(t *testing.T) {
	t.Parallel()

	trusted, err := clientip.ParseTrusted("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		remote  string
		trusted []netip.Prefix
		want    bool
	}{
		{"10.1.2.3:4567", trusted, true},
		{"[::ffff:10.1.2.3]:4567", trusted, true},
		{"192.0.2.1:4567", trusted, false},
		{"10.1.2.3:4567", nil, false},
		{"garbage", trusted, false},
	} {
		r := &http.Request{RemoteAddr: tc.remote}
		if got := clientip.PeerTrusted(r, tc.trusted); got != tc.want {
			t.Errorf("PeerTrusted(%q, %v) = %v, want %v", tc.remote, tc.trusted, got, tc.want)
		}
	}
}
