package web

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"

	"tailscale.com/tsnet"
)

// Tailnet puts the board on your tailnet as well as on loopback
// (DESIGN §20.4).
//
// gummi embeds the Tailscale node rather than requiring `tailscale serve`:
// a tsnet listener is its own node with its own name, so the board is
// reachable from a phone on mobile data with no port forwarding, no public
// listener, and no dependency on the host running tailscaled. Nothing else
// about the web face changes — pairing still gates every request, because
// a tailnet is a network boundary, not an identity check on the browser in
// your hand.
type Tailnet struct {
	srv *tsnet.Server
	// stop ends work the node started for itself (the certificate fetch).
	stop context.CancelFunc
	// Listener carries HTTP (or HTTPS with TLS) on the tailnet.
	Listener net.Listener
	// DNSName is the node's MagicDNS name, with its trailing dot removed.
	DNSName string
	// IPs are the node's tailnet addresses.
	IPs []netip.Addr
	// TLS says whether Listener terminates TLS with a tailnet certificate.
	TLS bool
	// Port is the port the listener is on.
	Port int
}

// TailnetOptions configures the embedded node.
type TailnetOptions struct {
	// Hostname is the node name on your tailnet; empty is "gummi".
	Hostname string
	// StateDir holds the node's identity between runs, so the board keeps
	// the same tailnet name and does not ask you to log in again.
	StateDir string
	// Port is the port to listen on; 0 picks 443 with TLS, 7878 without.
	Port int
	// TLS serves HTTPS with a tailnet certificate. It needs MagicDNS and
	// HTTPS certificates enabled for the tailnet, a one-time switch each in
	// the admin console.
	TLS bool
	// AuthKey authenticates the node without a browser login. Empty falls
	// back to TS_AUTHKEY (tsnet reads it), and then to an interactive login.
	AuthKey string
	// OnLogin is called with the login URL whenever the node needs a
	// person to add it to a tailnet, once per distinct URL. The node keeps
	// waiting for that login until ctx ends: the person is opening the URL
	// on another device, and that takes as long as it takes.
	OnLogin func(url string)
	// Logf reports what an operator should see about the node beyond its
	// login — a certificate that could not be fetched — and, with Verbose,
	// the node's own messages.
	Logf func(format string, args ...any)
	// Verbose passes the node's chatter — its user-facing log and its
	// backend's debug log — through to Logf.
	Verbose bool
}

// loginPoll is how often a node waiting for its first login is asked for
// the URL a person has to open. The URL appears a moment after the node
// starts and may be replaced while it waits.
const loginPoll = time.Second

// ListenTailnet brings the node up and starts listening. It blocks until
// the node is on the tailnet or ctx ends, reporting the login URL through
// OnLogin on a first run.
func ListenTailnet(ctx context.Context, o TailnetOptions) (*Tailnet, error) {
	if o.Hostname == "" {
		o.Hostname = "gummi"
	}
	if o.Port == 0 {
		if o.TLS {
			o.Port = 443
		} else {
			o.Port = 7878
		}
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.OnLogin == nil {
		o.OnLogin = func(string) {}
	}
	quiet := func(string, ...any) {}
	srv := &tsnet.Server{
		Hostname: o.Hostname,
		Dir:      o.StateDir,
		AuthKey:  o.AuthKey,
		// tsnet prints its login URL here every few seconds; OnLogin says
		// it once instead, so the node's own log is only for --verbose.
		UserLogf: quiet,
		Logf:     quiet,
	}
	if o.Verbose {
		logf := func(format string, args ...any) { o.Logf("tailscale: "+strings.TrimRight(format, "\n"), args...) }
		srv.UserLogf, srv.Logf = logf, logf
	}
	fail := func(err error) (*Tailnet, error) {
		_ = srv.Close()
		return nil, err
	}

	if err := srv.Start(); err != nil {
		// Close must not follow a failed Start (tsnet's contract).
		return nil, fmt.Errorf("starting the tailnet node: %w", err)
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return fail(fmt.Errorf("starting the tailnet node: %w", err))
	}
	watch, stopWatch := context.WithCancel(ctx)
	go func() {
		seen := ""
		tick := time.NewTicker(loginPoll)
		defer tick.Stop()
		for {
			if st, err := lc.StatusWithoutPeers(watch); err == nil && st.AuthURL != "" && st.AuthURL != seen {
				seen = st.AuthURL
				o.OnLogin(seen)
			}
			select {
			case <-watch.Done():
				return
			case <-tick.C:
			}
		}
	}()
	st, err := srv.Up(ctx)
	stopWatch()
	if err != nil {
		if ctx.Err() != nil {
			return fail(fmt.Errorf("joining your tailnet: %w", ctx.Err()))
		}
		return fail(fmt.Errorf("joining your tailnet: %w", err))
	}

	t := &Tailnet{srv: srv, TLS: o.TLS, Port: o.Port}
	if st != nil && st.Self != nil {
		t.DNSName = strings.TrimSuffix(st.Self.DNSName, ".")
		t.IPs = st.Self.TailscaleIPs
	}

	addr := fmt.Sprintf(":%d", o.Port)
	if o.TLS {
		t.Listener, err = srv.ListenTLS("tcp", addr)
	} else {
		t.Listener, err = srv.Listen("tcp", addr)
	}
	if err != nil {
		if o.TLS {
			return fail(fmt.Errorf("listening on %s with a tailnet certificate: %w "+
				"(--ts-tls needs MagicDNS and HTTPS certificates enabled for the tailnet)", addr, err))
		}
		return fail(fmt.Errorf("listening on %s of your tailnet: %w", addr, err))
	}
	if o.TLS && t.DNSName != "" {
		// The certificate is otherwise fetched on the first browser's
		// handshake, which then waits on Let's Encrypt; fetch it now, and
		// say so here if it cannot be had rather than in a browser's
		// certificate error.
		warm, stop := context.WithCancel(context.WithoutCancel(ctx))
		t.stop = stop
		go func() {
			if _, _, err := lc.CertPair(warm, t.DNSName); err != nil && warm.Err() == nil {
				o.Logf("tailscale: fetching the certificate for %s: %v", t.DNSName, err)
			}
		}()
	}
	return t, nil
}

// URL is where a browser on your tailnet reaches the board.
func (t *Tailnet) URL() string {
	scheme, host := "http", t.DNSName
	if t.TLS {
		scheme = "https"
	}
	if host == "" {
		if len(t.IPs) == 0 {
			return ""
		}
		host = t.IPs[0].String()
		if t.IPs[0].Is6() {
			host = "[" + host + "]"
		}
	}
	if (t.TLS && t.Port == 443) || (!t.TLS && t.Port == 80) {
		return scheme + "://" + host
	}
	return fmt.Sprintf("%s://%s:%d", scheme, host, t.Port)
}

// Close tears the node down.
func (t *Tailnet) Close() error {
	if t.stop != nil {
		t.stop()
	}
	if t.Listener != nil {
		_ = t.Listener.Close()
	}
	if t.srv != nil {
		return t.srv.Close()
	}
	return nil
}
