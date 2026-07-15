// Package headerprofile provides coherent outbound request profiles.
package headerprofile

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

var chromiumVersionPattern = regexp.MustCompile(`(?:Chrome|Chromium)/([0-9]+(?:\.[0-9]+){0,3})`)

// Name identifies a non-sensitive request header profile.
type Name string

var (
	// NameChromeDesktopPrimary identifies the primary configured desktop profile.
	NameChromeDesktopPrimary Name = "chrome_desktop_primary"
	// NameChromeDesktopSecondary identifies the secondary configured desktop profile.
	NameChromeDesktopSecondary Name = "chrome_desktop_secondary"
	// NameBaiduFixedSession identifies the stable HTTP identity used by Baidu's Cookie session.
	NameBaiduFixedSession Name = "baidu_fixed_session"
)

// BrandVersion is one Chromium user-agent client-hint brand entry.
type BrandVersion struct {
	Brand   string
	Version string
}

// ClientHints holds fields that must stay coherent with UserAgent and Platform.
type ClientHints struct {
	Brands          []BrandVersion
	FullVersionList []BrandVersion
	Platform        string
	PlatformVersion string
	Architecture    string
	Model           string
	Mobile          bool
	Bitness         string
}

// Profile is one internally consistent browser/HTTP request identity.
type Profile struct {
	Name              Name
	UserAgent         string
	AcceptLanguage    string
	Platform          string
	Headers           map[string]string
	ClientHints       *ClientHints
	ViewportWidth     int64
	ViewportHeight    int64
	DeviceScaleFactor float64
}

// Pool selects a sticky profile for a logical request and rotates by attempt.
type Pool interface {
	Select(key string, attempt int) (Profile, error)
}

// StaticPool is an immutable, concurrency-safe profile pool.
type StaticPool struct {
	profiles []Profile
}

// NewBaiduFixedSessionProfile builds the request identity verified by the low-frequency Cookie session baseline.
func NewBaiduFixedSessionProfile(userAgent string) (Profile, error) {
	pool, err := NewChromiumDesktopPool(userAgent)
	if err != nil {
		return Profile{}, err
	}
	profile := cloneProfile(pool.profiles[0])
	profile.Name = NameBaiduFixedSession
	profile.AcceptLanguage = "zh-CN,zh;q=0.9,en;q=0.8"
	delete(profile.Headers, "Upgrade-Insecure-Requests")
	return profile, nil
}

// NewChromiumDesktopPool builds two coherent desktop profiles around one
// deployment-controlled Chromium user agent. The variants use distinct but
// compatible desktop UAs, so a separately persisted Agent Profile can keep a
// stable complete browser identity rather than changing only its cookies.
func NewChromiumDesktopPool(userAgent string) (*StaticPool, error) {
	userAgent = strings.TrimSpace(userAgent)
	match := chromiumVersionPattern.FindStringSubmatch(userAgent)
	if len(match) != 2 {
		return nil, fmt.Errorf("user agent is not Chromium-compatible")
	}
	fullVersion := match[1]
	majorVersion := strings.Split(fullVersion, ".")[0]
	platform, hintPlatform, platformVersion, architecture := chromiumPlatform(userAgent)
	hints := &ClientHints{
		Brands: []BrandVersion{
			{Brand: "Chromium", Version: majorVersion},
			{Brand: "Google Chrome", Version: majorVersion},
			{Brand: "Not_A Brand", Version: "24"},
		},
		FullVersionList: []BrandVersion{
			{Brand: "Google Chrome", Version: fullVersion},
			{Brand: "Chromium", Version: fullVersion},
			{Brand: "Not_A Brand", Version: "24.0.0.0"},
		},
		Platform: hintPlatform, PlatformVersion: platformVersion,
		Architecture: architecture, Mobile: false, Bitness: "64",
	}
	commonHeaders := map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
		"Upgrade-Insecure-Requests": "1",
	}
	secondaryUserAgent := alternateDesktopUserAgent(userAgent)
	secondaryPlatform, secondaryHintPlatform, secondaryPlatformVersion, secondaryArchitecture := chromiumPlatform(secondaryUserAgent)
	secondaryHints := cloneClientHints(hints)
	secondaryHints.Platform = secondaryHintPlatform
	secondaryHints.PlatformVersion = secondaryPlatformVersion
	secondaryHints.Architecture = secondaryArchitecture
	return NewStaticPool([]Profile{
		{
			Name: NameChromeDesktopPrimary, UserAgent: userAgent,
			AcceptLanguage: "zh-CN,zh;q=0.9,en;q=0.7", Platform: platform,
			Headers: commonHeaders, ClientHints: hints,
			ViewportWidth: 1440, ViewportHeight: 900, DeviceScaleFactor: 2,
		},
		{
			Name: NameChromeDesktopSecondary, UserAgent: secondaryUserAgent,
			AcceptLanguage: "en-US,en;q=0.9,zh-CN;q=0.7", Platform: secondaryPlatform,
			Headers: commonHeaders, ClientHints: secondaryHints,
			ViewportWidth: 1366, ViewportHeight: 768, DeviceScaleFactor: 1,
		},
	})
}

func alternateDesktopUserAgent(userAgent string) string {
	const macOS = "(Macintosh; Intel Mac OS X 10_15_7)"
	const windows = "(Windows NT 10.0; Win64; x64)"
	if strings.Contains(userAgent, macOS) {
		return strings.Replace(userAgent, macOS, windows, 1)
	}
	if strings.Contains(userAgent, windows) {
		return strings.Replace(userAgent, windows, macOS, 1)
	}
	return userAgent
}

func cloneClientHints(hints *ClientHints) *ClientHints {
	clone := *hints
	clone.Brands = append([]BrandVersion(nil), hints.Brands...)
	clone.FullVersionList = append([]BrandVersion(nil), hints.FullVersionList...)
	return &clone
}

func chromiumPlatform(userAgent string) (platform string, hintPlatform string, platformVersion string, architecture string) {
	switch {
	case strings.Contains(userAgent, "Windows NT"):
		return "Win32", "Windows", "10.0.0", "x86"
	case strings.Contains(userAgent, "Macintosh"):
		return "MacIntel", "macOS", "15.0.0", "arm"
	default:
		return "Linux x86_64", "Linux", "", "x86"
	}
}

// NewStaticPool validates and clones profiles.
func NewStaticPool(profiles []Profile) (*StaticPool, error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("header profile pool is empty")
	}
	cloned := make([]Profile, len(profiles))
	seen := make(map[Name]struct{}, len(profiles))
	for index, profile := range profiles {
		profile.Name = Name(strings.TrimSpace(string(profile.Name)))
		profile.UserAgent = strings.TrimSpace(profile.UserAgent)
		profile.AcceptLanguage = strings.TrimSpace(profile.AcceptLanguage)
		if profile.Name == "" {
			return nil, fmt.Errorf("header profile %d name is empty", index)
		}
		if _, exists := seen[profile.Name]; exists {
			return nil, fmt.Errorf("duplicate header profile name %q", profile.Name)
		}
		if profile.UserAgent == "" {
			return nil, fmt.Errorf("header profile %q user agent is empty", profile.Name)
		}
		if profile.AcceptLanguage == "" {
			return nil, fmt.Errorf("header profile %q accept language is empty", profile.Name)
		}
		seen[profile.Name] = struct{}{}
		cloned[index] = cloneProfile(profile)
	}
	return &StaticPool{profiles: cloned}, nil
}

// Select returns a stable profile for key. Incrementing attempt walks the pool.
func (p *StaticPool) Select(key string, attempt int) (Profile, error) {
	if p == nil || len(p.profiles) == 0 {
		return Profile{}, fmt.Errorf("header profile pool is not initialized")
	}
	if attempt < 0 {
		return Profile{}, fmt.Errorf("header profile attempt must not be negative")
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(key))
	index := (int(hash.Sum64()%uint64(len(p.profiles))) + attempt) % len(p.profiles)
	return cloneProfile(p.profiles[index]), nil
}

func cloneProfile(profile Profile) Profile {
	clone := profile
	if profile.Headers != nil {
		clone.Headers = make(map[string]string, len(profile.Headers))
		for key, value := range profile.Headers {
			clone.Headers[key] = value
		}
	}
	if profile.ClientHints != nil {
		clone.ClientHints = cloneClientHints(profile.ClientHints)
	}
	return clone
}
