package headerprofile

import "testing"

func TestStaticPoolSelectsStickyProfileAndRotatesByAttempt(t *testing.T) {
	pool, err := NewStaticPool([]Profile{
		{Name: "chrome_macos_150", UserAgent: "ua-mac", AcceptLanguage: "zh-CN,zh;q=0.9"},
		{Name: "chrome_windows_150", UserAgent: "ua-windows", AcceptLanguage: "zh-CN,zh;q=0.9"},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := pool.Select("request-42", 0)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := pool.Select("request-42", 0)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := pool.Select("request-42", 1)
	if err != nil {
		t.Fatal(err)
	}

	if first.Name != repeated.Name {
		t.Fatalf("sticky selection changed: %q != %q", first.Name, repeated.Name)
	}
	if first.Name == rotated.Name {
		t.Fatalf("attempt did not rotate profile: %q", first.Name)
	}
}

func TestStaticPoolRejectsIncoherentProfiles(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		profile Profile
	}{
		{name: "missing name", profile: Profile{UserAgent: "ua"}},
		{name: "missing user agent", profile: Profile{Name: "profile"}},
		{name: "missing language", profile: Profile{Name: "profile", UserAgent: "ua"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := NewStaticPool([]Profile{testCase.profile}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestStaticPoolReturnsIndependentHeaderCopies(t *testing.T) {
	pool, err := NewStaticPool([]Profile{{
		Name: "profile", UserAgent: "ua", AcceptLanguage: "zh-CN",
		Headers: map[string]string{"Accept": "text/html"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := pool.Select("request", 0)
	first.Headers["Accept"] = "changed"
	second, _ := pool.Select("request", 0)
	if second.Headers["Accept"] != "text/html" {
		t.Fatalf("stored profile was mutated: %#v", second.Headers)
	}
}

func TestNewBaiduFixedSessionProfileMatchesVerifiedBaseline(t *testing.T) {
	profile, err := NewBaiduFixedSessionProfile("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.115 Safari/537.36")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Name != NameBaiduFixedSession || profile.AcceptLanguage != "zh-CN,zh;q=0.9,en;q=0.8" {
		t.Fatalf("profile=%+v", profile)
	}
	if _, exists := profile.Headers["Upgrade-Insecure-Requests"]; exists {
		t.Fatalf("fixed Baidu profile contains Upgrade-Insecure-Requests: %+v", profile.Headers)
	}
}

func TestNewChromiumDesktopPoolBuildsCoherentClientHints(t *testing.T) {
	pool, err := NewChromiumDesktopPool("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.115 Safari/537.36")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := pool.Select("request", 0)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Platform != "MacIntel" || profile.ClientHints == nil || profile.ClientHints.Platform != "macOS" {
		t.Fatalf("profile=%+v", profile)
	}
	if len(profile.ClientHints.FullVersionList) == 0 || profile.ClientHints.FullVersionList[0].Version != "150.0.7871.115" {
		t.Fatalf("client hints=%+v", profile.ClientHints)
	}
}

func TestNewChromiumDesktopPoolUsesDistinctCoherentUserAgents(t *testing.T) {
	pool, err := NewChromiumDesktopPool("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.115 Safari/537.36")
	if err != nil {
		t.Fatal(err)
	}
	primary := pool.profiles[0]
	secondary := pool.profiles[1]
	if primary.UserAgent == secondary.UserAgent {
		t.Fatalf("profiles must use distinct user agents: %q", primary.UserAgent)
	}
	if secondary.Platform != "Win32" || secondary.ClientHints == nil || secondary.ClientHints.Platform != "Windows" {
		t.Fatalf("secondary profile is not Windows-coherent: %+v", secondary)
	}
}

func TestNewChromiumDesktopPoolRejectsNonChromiumUserAgent(t *testing.T) {
	if _, err := NewChromiumDesktopPool("curl/8.0"); err == nil {
		t.Fatal("expected Chromium user agent validation error")
	}
}

func TestNewChromiumDesktopPoolUsesWindowsArchitecture(t *testing.T) {
	pool, err := NewChromiumDesktopPool("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.7871.115 Safari/537.36")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := pool.Select("request", 0)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Platform != "Win32" || profile.ClientHints.Architecture != "x86" || profile.ClientHints.Platform != "Windows" {
		t.Fatalf("profile=%+v hints=%+v", profile, profile.ClientHints)
	}
}
