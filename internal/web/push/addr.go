package push

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"syscall"
)

// A push endpoint is a URL a browser handed the host, and the host POSTs
// to it on its own schedule. That makes it a way to point the host at any
// address it can reach — the machine's own services, the LAN, the cloud's
// metadata address, the tailnet — so an endpoint must name a public host,
// and that is checked twice: when a device subscribes, so a bad one is
// refused to the page that sent it, and again when the host dials, so a
// name that resolved somewhere public at subscribe time cannot be
// re-pointed inward later (DNS rebinding).

// ErrPrivateEndpoint is the refusal of an endpoint that is, or resolves
// to, an address the host must not send to.
var ErrPrivateEndpoint = errors.New("push: endpoint must be a public push service")

// cgnat is 100.64.0.0/10: carrier-grade NAT, and the range a tailnet's
// addresses come from — as internal as any RFC 1918 range here.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicAddr reports whether a is an address the host may send a push to.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	switch {
	case !a.IsValid(),
		a.IsLoopback(),
		a.IsPrivate(),
		a.IsUnspecified(),
		a.IsLinkLocalUnicast(),
		a.IsLinkLocalMulticast(),
		a.IsInterfaceLocalMulticast(),
		a.IsMulticast(),
		a.Is4() && cgnat.Contains(a):
		return false
	}
	return true
}

// CheckEndpoint is the subscribe-time half: the endpoint names a host (not
// an address), and every address that host resolves to now is public.
// allowPrivate skips it, for a test's stand-in push service on loopback.
//
// A name that does not resolve at all is let through: it cannot be sent to
// either, and the dial-time check (Sender) still stands between it and
// whatever it resolves to later.
func CheckEndpoint(ctx context.Context, endpoint string, allowPrivate bool) error {
	if allowPrivate {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("push: endpoint: %w", err)
	}
	host := u.Hostname()
	if _, err := netip.ParseAddr(host); err == nil {
		return fmt.Errorf("%w: %q names an address, not a push service", ErrPrivateEndpoint, host)
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if !publicAddr(a) {
			return fmt.Errorf("%w: %s resolves to %s", ErrPrivateEndpoint, host, a.Unmap())
		}
	}
	return nil
}

// dialControl is the dial-time half: it runs after the name is resolved
// and before the socket connects, so it sees the address actually dialled.
func dialControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: cannot tell where %q is", ErrPrivateEndpoint, address)
	}
	if !publicAddr(ap.Addr()) {
		return fmt.Errorf("%w: refusing to connect to %s", ErrPrivateEndpoint, ap.Addr().Unmap())
	}
	return nil
}
