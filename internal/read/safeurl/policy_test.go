package safeurl

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type stubResolver struct {
	addresses []netip.Addr
	err       error
}

func (r stubResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, r.err
}

func TestPolicyRejectsInvalidURLForms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{name: "scheme", url: "file:///etc/passwd"},
		{name: "userinfo", url: "https://user:pass@example.com/"},
		{name: "missing host", url: "https:///article"},
		{name: "invalid port", url: "https://example.com:70000/article"},
	}

	policy := NewPolicy(stubResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}, Config{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := policy.ValidateAndResolve(context.Background(), tt.url)
			if !errors.Is(err, ErrUnsafeURL) {
				t.Fatalf("ValidateAndResolve() error = %v, want ErrUnsafeURL", err)
			}
		})
	}
}

func TestPolicyRejectsEveryUnsafeAddressClass(t *testing.T) {
	t.Parallel()

	addresses := []string{
		"127.0.0.1",            // loopback
		"10.0.0.1",             // private
		"169.254.1.1",          // link local
		"224.0.0.1",            // multicast
		"0.0.0.0",              // unspecified
		"100.64.0.1",           // CGNAT
		"169.254.169.254",      // cloud metadata
		"::1",                  // IPv6 loopback
		"fc00::1",              // IPv6 private
		"fe80::1",              // IPv6 link local
		"ff02::1",              // IPv6 multicast
		"::",                   // IPv6 unspecified
		"64:ff9b::a9fe:a9fe",   // NAT64-encoded metadata IPv4
		"64:ff9b:1::a9fe:a9fe", // local-use NAT64 prefix
		"2002:a9fe:a9fe::1",    // 6to4-encoded metadata IPv4
	}

	for _, rawAddress := range addresses {
		t.Run(rawAddress, func(t *testing.T) {
			policy := NewPolicy(stubResolver{addresses: []netip.Addr{netip.MustParseAddr(rawAddress)}}, Config{})
			_, err := policy.ValidateAndResolve(context.Background(), "https://example.com/article")
			if !errors.Is(err, ErrUnsafeURL) {
				t.Fatalf("ValidateAndResolve() error = %v, want ErrUnsafeURL", err)
			}
		})
	}
}

func TestPolicyRejectsMixedPublicAndPrivateDNSAnswers(t *testing.T) {
	t.Parallel()

	policy := NewPolicy(stubResolver{addresses: []netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("192.168.1.10"),
	}}, Config{})

	_, err := policy.ValidateAndResolve(context.Background(), "https://example.com/article")
	if !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("ValidateAndResolve() error = %v, want ErrUnsafeURL", err)
	}
}

func TestPolicyReturnsPinnedPublicTarget(t *testing.T) {
	t.Parallel()

	policy := NewPolicy(stubResolver{addresses: []netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("2606:2800:220:1:248:1893:25c8:1946"),
	}}, Config{})

	target, err := policy.ValidateAndResolve(context.Background(), "https://Example.COM:8443/article?q=go#section")
	if err != nil {
		t.Fatalf("ValidateAndResolve() error = %v", err)
	}
	if got, want := target.URL.String(), "https://example.com:8443/article?q=go"; got != want {
		t.Fatalf("target.URL = %q, want %q", got, want)
	}
	if got, want := target.Addresses[0].String(), "93.184.216.34"; got != want {
		t.Fatalf("target.Addresses[0] = %q, want %q", got, want)
	}
	if got, want := target.URL.Hostname(), "example.com"; got != want {
		t.Fatalf("target.URL.Hostname() = %q, want %q", got, want)
	}
}

func TestPolicyAllowsPrivateAddressOnlyForExactAllowlistedHost(t *testing.T) {
	t.Parallel()

	resolver := stubResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	policy := NewPolicy(resolver, Config{AllowedHosts: []string{"demo.local"}})

	if _, err := policy.ValidateAndResolve(context.Background(), "http://demo.local:8080/article"); err != nil {
		t.Fatalf("allowlisted ValidateAndResolve() error = %v", err)
	}
	if _, err := policy.ValidateAndResolve(context.Background(), "http://sub.demo.local:8080/article"); !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("subdomain ValidateAndResolve() error = %v, want ErrUnsafeURL", err)
	}
}

func TestPolicyAllowlistDoesNotPermitMetadataOrNonUnicastAddresses(t *testing.T) {
	t.Parallel()

	for _, rawAddress := range []string{"169.254.169.254", "224.0.0.1", "0.0.0.0", "ff02::1", "::"} {
		t.Run(rawAddress, func(t *testing.T) {
			policy := NewPolicy(stubResolver{addresses: []netip.Addr{netip.MustParseAddr(rawAddress)}}, Config{AllowedHosts: []string{"demo.local"}})
			_, err := policy.ValidateAndResolve(context.Background(), "http://demo.local/article")
			if !errors.Is(err, ErrUnsafeURL) {
				t.Fatalf("ValidateAndResolve() error = %v, want ErrUnsafeURL", err)
			}
		})
	}
}

func TestPolicyPreservesResolverError(t *testing.T) {
	t.Parallel()

	original := errors.New("resolver exploded")
	policy := NewPolicy(stubResolver{err: original}, Config{})

	_, err := policy.ValidateAndResolve(context.Background(), "https://example.com/article")
	if !errors.Is(err, original) {
		t.Fatalf("ValidateAndResolve() error = %v, want wrapped original error", err)
	}
	if !errors.Is(err, ErrResolveFailed) {
		t.Fatalf("ValidateAndResolve() error = %v, want ErrResolveFailed", err)
	}
}
