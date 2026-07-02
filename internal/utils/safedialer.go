// Safe outbound HTTP for endpoints that fetch user- or plugin-supplied URLs
// (the subtitle proxy, Nuvio scraper fetch). The dialer refuses connections
// to loopback, private, and link-local addresses so untrusted URLs can't be
// used to reach the local API or anything else on the user's network (SSRF).
// The check runs at dial time, after DNS resolution, which also defeats
// DNS-rebinding tricks; redirects re-dial and are therefore covered too.
package utils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// ValidatePublicURL rejects URLs that aren't plain http(s). Host checks
// happen at dial time in SafeTransport, not here.
func ValidatePublicURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing host")
	}
	return u, nil
}

func isPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	return !(addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		addr.IsInterfaceLocalMulticast())
}

// SafeTransport returns an http.Transport that only dials public addresses.
func SafeTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !isPublicAddr(ip) {
					return nil, fmt.Errorf("refusing to connect to non-public address %s", ip)
				}
			}
			// Dial one of the vetted IPs directly rather than re-resolving,
			// so a rebinding DNS server can't swap the answer under us.
			var lastErr error
			for _, ip := range ips {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          20,
		IdleConnTimeout:       60 * time.Second,
	}
}

// SafeHTTPClient is a shared client for fetching untrusted public URLs:
// public-address-only dialing plus an overall request timeout.
var SafeHTTPClient = &http.Client{
	Transport: SafeTransport(),
	Timeout:   30 * time.Second,
}
