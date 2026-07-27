package tunnelnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

var lookupSystemHost = net.DefaultResolver.LookupHost
var lookupPublicHost = cloudflarePublicResolver().LookupHost

func LookupHost(ctx context.Context, host string) ([]string, error) {
	addresses, err := lookupSystemHost(ctx, host)
	if err == nil {
		return addresses, nil
	}

	publicAddresses, publicErr := lookupPublicHost(ctx, host)
	if publicErr != nil {
		return nil, fmt.Errorf("system resolver: %v; Cloudflare public DNS: %w", err, publicErr)
	}
	return publicAddresses, nil
}

func HTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = DialContext
	return &http.Client{Transport: transport}
}

func NoRedirectHTTPClient() *http.Client {
	client := HTTPClient()
	client.CheckRedirect = RejectRedirect
	return client
}

func RejectRedirect(request *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func DialContext(ctx context.Context, network string, address string) (net.Conn, error) {
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, network, address)
	if err == nil {
		return connection, nil
	}
	if !isDNSError(err) {
		return nil, err
	}

	host, port, splitErr := net.SplitHostPort(address)
	if splitErr != nil {
		return nil, err
	}
	addresses, lookupErr := LookupHost(ctx, host)
	if lookupErr != nil {
		return nil, fmt.Errorf("system resolver: %v; Cloudflare public DNS: %w", err, lookupErr)
	}

	var lastDialErr error
	for _, resolved := range addresses {
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved, port))
		if dialErr == nil {
			return connection, nil
		}
		lastDialErr = dialErr
	}
	return nil, fmt.Errorf("system resolver: %v; resolved fallback addresses were unreachable: %w", err, lastDialErr)
}

func isDNSError(err error) bool {
	var dnsError *net.DNSError
	return errors.As(err, &dnsError)
}

func cloudflarePublicResolver() *net.Resolver {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	addresses := []string{"1.1.1.1:53", "1.0.0.1:53"}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network string, address string) (net.Conn, error) {
			var lastErr error
			for _, candidate := range addresses {
				connection, err := dialer.DialContext(ctx, network, candidate)
				if err == nil {
					return connection, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
}
