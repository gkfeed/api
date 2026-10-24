package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// Metadata requests bypass environment proxies and dial only the public IPs
// checked here. Resolving once and dialing the numeric IP prevents DNS rebinding.
func newPublicHTTPClient() *http.Client {
	transport := &http.Transport{
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 4 * time.Second,
		MaxConnsPerHost:       8,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if port != "80" && port != "443" {
				return nil, errors.New("metadata port is not allowed")
			}
			addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			if len(addresses) == 0 {
				return nil, errors.New("metadata host has no addresses")
			}
			for _, ip := range addresses {
				if !isPublicIP(ip) {
					return nil, errors.New("metadata host is not public")
				}
			}
			dialer := net.Dialer{Timeout: 3 * time.Second}
			for _, ip := range addresses {
				connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if dialErr == nil {
					return connection, nil
				}
				err = dialErr
			}
			return nil, err
		},
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many metadata redirects")
			}
			_, err := parseFeedURL(request.URL.String())
			return err
		},
	}
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2002::/16"),
}

func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
