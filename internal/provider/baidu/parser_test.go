package baidu

import (
	"os"
	"testing"
)

func TestParseDesktop(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/baidu/desktop_normal.html")
	if err != nil {
		t.Fatal(err)
	}
	got, warnings, err := ParseDesktop(body, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("results=%#v", got)
	}
	if got[0].Title != "The Go Programming Language" || got[0].URL != "https://go.dev/" || got[0].Rank != 1 {
		t.Fatalf("first=%#v", got[0])
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings=%#v", warnings)
	}
}

func TestParseMobileDataLogURL(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/baidu/mobile_normal.html")
	if err != nil {
		t.Fatal(err)
	}
	got, warnings, err := ParseMobile(body, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://go.dev/" || got[0].Snippet == "" {
		t.Fatalf("results=%#v", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings=%#v", warnings)
	}
}

func TestParseDesktopReturnsRedirectWarning(t *testing.T) {
	body := []byte(`<div id="content_left"><div class="result"><h3><a href="https://www.baidu.com/link?url=abc">Title</a></h3><div class="c-abstract">Text</div></div></div>`)
	got, warnings, err := ParseDesktop(body, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("results=%#v err=%v", got, err)
	}
	if len(warnings) != 1 || warnings[0].Code != "redirect_url_unresolved" {
		t.Fatalf("warnings=%#v", warnings)
	}
}

func TestParseDesktopAcceptsConfirmedEmptyPage(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/baidu/empty.html")
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := ParseDesktop(body, 10)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("results=%#v err=%v", got, err)
	}
}

func TestParseDesktopRejectsUnknownPage(t *testing.T) {
	if _, _, err := ParseDesktop([]byte(`<html>unknown</html>`), 10); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseDesktopOnlyUsesTopLevelResultCards(t *testing.T) {
	body := []byte(`
<div id="content_left">
  <div class="result-op c-container" mu="http://nourl.ubs.baidu.com/61344">
    <div class="c-container" mu="https://wrong.example/"><h3><a href="https://wrong.example/">Nested answer link</a></h3></div>
  </div>
  <div class="result c-container" mu="https://go.dev/">
    <h3><a href="https://www.baidu.com/link?url=go">The Go Programming Language</a></h3>
    <div class="summary-text_15QGa">Go makes it easy to build simple, secure, scalable systems.</div>
  </div>
</div>`)
	got, _, err := ParseDesktop(body, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://go.dev/" || got[0].Snippet == "" {
		t.Fatalf("results=%#v", got)
	}
}

func TestParseDesktopSkipsBaiduRecommendationAds(t *testing.T) {
	body := []byte(`
<div id="content_left">
  <div class="result-op c-container" mu="http://28608.recommend_list.baidu.com">
    <h3><a href="http://28608.recommend_list.baidu.com">python一般要学几年</a></h3>
  </div>
  <div class="result c-container" mu="https://go.dev/">
    <h3><a href="https://go.dev/">The Go Programming Language</a></h3>
  </div>
</div>`)
	got, _, err := ParseDesktop(body, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].URL != "https://go.dev/" {
		t.Fatalf("results=%#v", got)
	}
}
