package ai

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// maxBody caps how much of a response the client reads.
const maxBody = 8 << 20

var (
	alwaysBlocked = prefixes("169.254.0.0/16", "fe80::/10", "fd00:ec2::254/128") // link-local, cloud metadata
	privateRanges = prefixes("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "fc00::/7")
)

func prefixes(s ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range s {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}

type blockedError struct{ addr netip.Addr }

func (e *blockedError) Error() string { return "blocked address " + e.addr.String() }

// checkAddr rejects addresses AI endpoints may not use on this server.
func checkAddr(ip netip.Addr, blockPrivate bool) error {
	ip = ip.Unmap()
	bad := ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
	for _, p := range alwaysBlocked {
		bad = bad || p.Contains(ip)
	}
	if blockPrivate {
		bad = bad || ip.IsLoopback() || ip.IsPrivate()
		for _, p := range privateRanges {
			bad = bad || p.Contains(ip)
		}
	}
	if bad {
		return &blockedError{ip}
	}
	return nil
}

// NewHTTPClient returns a client that refuses to connect to link-local and
// cloud-metadata addresses, and, when blockPrivate is true, to loopback,
// private, CGNAT and unique-local addresses too. The check runs on the
// resolved address at connect time, so DNS rebinding and redirects are covered.
// Environment proxies are ignored, since they would hide the real target.
func NewHTTPClient(blockPrivate bool) *http.Client {
	d := &net.Dialer{
		Timeout:   15 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return &blockedError{}
			}
			return checkAddr(ap.Addr(), blockPrivate)
		},
	}
	return &http.Client{Transport: &http.Transport{
		DialContext:         d.DialContext,
		TLSHandshakeTimeout: 15 * time.Second,
		MaxIdleConns:        10,
		IdleConnTimeout:     60 * time.Second,
	}}
}

func asBlocked(err error) *Error {
	var be *blockedError
	if errors.As(err, &be) {
		return &Error{Kind: KindUnreachable, Message: "This address is not allowed for AI endpoints on this server. Use an endpoint on a public address."}
	}
	return nil
}
