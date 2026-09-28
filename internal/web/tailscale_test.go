package web

import (
	"net/netip"
	"testing"
)

// Bringing a tsnet node up needs a tailnet and a login, so what is tested
// here is the part a person reads: the URL printed in the terminal, which
// has to be the one a browser can actually open.
func TestTailnetURL(t *testing.T) {
	cases := []struct {
		name string
		net  Tailnet
		want string
	}{
		{
			name: "https on the default port drops the port",
			net:  Tailnet{DNSName: "gummi.tail1234.ts.net", TLS: true, Port: 443},
			want: "https://gummi.tail1234.ts.net",
		},
		{
			name: "http on a chosen port keeps it",
			net:  Tailnet{DNSName: "gummi.tail1234.ts.net", Port: 7878},
			want: "http://gummi.tail1234.ts.net:7878",
		},
		{
			name: "no MagicDNS name falls back to the tailnet address",
			net:  Tailnet{IPs: []netip.Addr{netip.MustParseAddr("100.64.0.7")}, Port: 7878},
			want: "http://100.64.0.7:7878",
		},
		{
			name: "an IPv6 tailnet address is bracketed",
			net:  Tailnet{IPs: []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::7")}, Port: 7878},
			want: "http://[fd7a:115c:a1e0::7]:7878",
		},
		{
			name: "https on a chosen port keeps it too",
			net:  Tailnet{DNSName: "gummi.tail1234.ts.net", TLS: true, Port: 8443},
			want: "https://gummi.tail1234.ts.net:8443",
		},
		{
			name: "nothing to say yet",
			net:  Tailnet{Port: 7878},
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.net.URL(); got != c.want {
				t.Errorf("URL() = %q, want %q", got, c.want)
			}
		})
	}
}

// Close on a node that never came up must not panic — `gummi web` defers it
// on every path, including the ones where Listen failed.
func TestTailnetCloseOnAnEmptyNode(t *testing.T) {
	var tn Tailnet
	if err := tn.Close(); err != nil {
		t.Errorf("Close on an empty node = %v, want nil", err)
	}
}
