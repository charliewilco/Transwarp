package tunnelnet

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestLookupHostUsesSystemResolverFirst(t *testing.T) {
	stubLookups(t, []string{"192.0.2.10"}, nil, []string{"203.0.113.10"}, nil)

	addresses, err := LookupHost(context.Background(), "transwarp.example.com")
	if err != nil {
		t.Fatalf("LookupHost returned error: %v", err)
	}
	if len(addresses) != 1 || addresses[0] != "192.0.2.10" {
		t.Fatalf("unexpected addresses: %+v", addresses)
	}
}

func TestLookupHostFallsBackToCloudflarePublicDNS(t *testing.T) {
	stubLookups(
		t,
		nil,
		&net.DNSError{Name: "quick.trycloudflare.com", IsNotFound: true},
		[]string{"203.0.113.10"},
		nil,
	)

	addresses, err := LookupHost(context.Background(), "quick.trycloudflare.com")
	if err != nil {
		t.Fatalf("LookupHost returned error: %v", err)
	}
	if len(addresses) != 1 || addresses[0] != "203.0.113.10" {
		t.Fatalf("unexpected addresses: %+v", addresses)
	}
}

func TestLookupHostReportsBothResolverFailures(t *testing.T) {
	stubLookups(
		t,
		nil,
		&net.DNSError{Name: "quick.trycloudflare.com", IsNotFound: true},
		nil,
		&net.DNSError{Name: "quick.trycloudflare.com", IsTimeout: true},
	)

	_, err := LookupHost(context.Background(), "quick.trycloudflare.com")
	if err == nil {
		t.Fatal("expected lookup error")
	}
	if !strings.Contains(err.Error(), "system resolver") || !strings.Contains(err.Error(), "Cloudflare public DNS") {
		t.Fatalf("expected both resolver failures, got %v", err)
	}
}

func TestDialContextUsesFallbackResolvedAddress(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		if connection != nil {
			accepted <- connection
		}
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	stubLookups(
		t,
		nil,
		&net.DNSError{Name: "fallback.invalid", IsNotFound: true},
		[]string{"127.0.0.1"},
		nil,
	)

	connection, err := DialContext(context.Background(), "tcp", net.JoinHostPort("fallback.invalid", port))
	if err != nil {
		t.Fatalf("DialContext returned error: %v", err)
	}
	connection.Close()

	select {
	case acceptedConnection := <-accepted:
		acceptedConnection.Close()
	case <-time.After(time.Second):
		t.Fatal("listener did not accept fallback connection")
	}
}

func stubLookups(t *testing.T, systemAddresses []string, systemErr error, publicAddresses []string, publicErr error) {
	t.Helper()

	originalSystem := lookupSystemHost
	originalPublic := lookupPublicHost
	lookupSystemHost = func(ctx context.Context, host string) ([]string, error) {
		return systemAddresses, systemErr
	}
	lookupPublicHost = func(ctx context.Context, host string) ([]string, error) {
		return publicAddresses, publicErr
	}
	t.Cleanup(func() {
		lookupSystemHost = originalSystem
		lookupPublicHost = originalPublic
	})
}
