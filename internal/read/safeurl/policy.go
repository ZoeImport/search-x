// Package safeurl validates and resolves outbound URLs without trusting a later
// DNS lookup performed by the HTTP transport.
package safeurl

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"web-search-backend/internal/domain"
)

const defaultMaxHostLength = 253

var (
	// ErrUnsafeURL is returned when a URL or one of its resolved addresses is unsafe.
	ErrUnsafeURL = errors.New("unsafe URL")
	// ErrResolveFailed is returned when a target hostname cannot be resolved.
	ErrResolveFailed = errors.New("resolve target")
)

// Resolver resolves all IP addresses for a hostname.
type Resolver interface {
	// LookupNetIP returns every address for host in deterministic resolver order.
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Config configures the URL safety policy.
type Config struct {
	// AllowedHosts contains exact, case-insensitive hostnames whose private
	// or loopback addresses may be used. It never permits metadata, link-local,
	// multicast, unspecified, or otherwise invalid targets.
	AllowedHosts []string
	// MaxHostLength bounds the hostname length. Zero uses 253 bytes.
	MaxHostLength int
}

// PolicyError retains the stable classification and original failure.
type PolicyError struct {
	// Code is the stable read-pipeline classification.
	Code domain.ErrorCode
	// URL is the rejected or unresolved target URL.
	URL string
	// Original retains the low-level validation or resolver failure.
	Original error
}

// Error returns the original error text for development diagnostics.
func (e *PolicyError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %v", e.URL, e.Original)
}

// Unwrap exposes both the stable sentinel and the original error.
func (e *PolicyError) Unwrap() []error {
	if e == nil {
		return nil
	}
	if e.Code == domain.ErrFetchFailed {
		return []error{ErrResolveFailed, e.Original}
	}
	return []error{ErrUnsafeURL, e.Original}
}

// ReadErrorCode returns the stable read-pipeline classification.
func (e *PolicyError) ReadErrorCode() domain.ErrorCode {
	if e == nil {
		return domain.ErrFetchFailed
	}
	return e.Code
}

// Policy validates URLs and resolves every DNS answer before selecting one.
type Policy struct {
	resolver      Resolver
	allowedHosts  map[string]struct{}
	maxHostLength int
}

// NewPolicy constructs an immutable URL policy.
func NewPolicy(resolver Resolver, cfg Config) *Policy {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	maxHostLength := cfg.MaxHostLength
	if maxHostLength == 0 {
		maxHostLength = defaultMaxHostLength
	}
	allowedHosts := make(map[string]struct{}, len(cfg.AllowedHosts))
	for _, host := range cfg.AllowedHosts {
		host = canonicalHost(host)
		if host != "" {
			allowedHosts[host] = struct{}{}
		}
	}
	return &Policy{resolver: resolver, allowedHosts: allowedHosts, maxHostLength: maxHostLength}
}

// ValidateAndResolve validates rawURL, verifies every resolved address, and
// returns a target pinned to the policy-approved resolver answers.
func (p *Policy) ValidateAndResolve(ctx context.Context, rawURL string) (domain.SafeTarget, error) {
	targetURL, host, _, err := p.validateURL(rawURL)
	if err != nil {
		return domain.SafeTarget{}, unsafeError(rawURL, err)
	}

	addresses, err := resolve(ctx, p.resolver, host)
	if err != nil {
		return domain.SafeTarget{}, &PolicyError{Code: domain.ErrFetchFailed, URL: rawURL, Original: err}
	}
	_, allowPrivate := p.allowedHosts[host]
	for _, address := range addresses {
		if !isPublicAddress(address) && !(allowPrivate && isAllowlistedAddress(address)) {
			return domain.SafeTarget{}, unsafeError(rawURL, fmt.Errorf("resolved address %s is not public", address))
		}
	}
	pinnedAddresses := make([]net.IP, len(addresses))
	for index, address := range addresses {
		pinnedAddresses[index] = net.IP(address.AsSlice())
	}
	return domain.SafeTarget{URL: targetURL, Addresses: pinnedAddresses}, nil
}

func (p *Policy) validateURL(rawURL string) (*url.URL, string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, "", "", fmt.Errorf("scheme %q is not allowed", parsed.Scheme)
	}
	if parsed.User != nil {
		return nil, "", "", errors.New("userinfo is not allowed")
	}
	host := canonicalHost(parsed.Hostname())
	if host == "" {
		return nil, "", "", errors.New("host is required")
	}
	if len(host) > p.maxHostLength {
		return nil, "", "", fmt.Errorf("host exceeds %d bytes", p.maxHostLength)
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else {
		portNumber, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil || portNumber == 0 {
			return nil, "", "", fmt.Errorf("invalid port %q", port)
		}
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Fragment = ""
	if parsed.Port() != "" {
		parsed.Host = net.JoinHostPort(host, parsed.Port())
	} else if strings.Contains(host, ":") {
		parsed.Host = "[" + host + "]"
	} else {
		parsed.Host = host
	}
	return parsed, host, port, nil
}

func resolve(ctx context.Context, resolver Resolver, host string) ([]netip.Addr, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{literal.Unmap()}, nil
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("DNS returned no addresses")
	}
	for index := range addresses {
		addresses[index] = addresses[index].Unmap()
	}
	return addresses, nil
}

func canonicalHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func unsafeError(rawURL string, original error) error {
	return &PolicyError{Code: domain.ErrUnsafeURL, URL: rawURL, Original: original}
}

func isPublicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func isAllowlistedAddress(address netip.Addr) bool {
	address = address.Unmap()
	return address.IsValid() && (address.IsPrivate() || address.IsLoopback())
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001:db8::/32"),
}
