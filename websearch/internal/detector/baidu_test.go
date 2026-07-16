package detector

import "testing"

func TestClassifyCaptchaByBody(t *testing.T) {
	body := []byte(`<title>安全验证</title><form id="verify-form">请输入验证码</form>`)
	if got := Classify(200, "https://www.baidu.com/s", "安全验证", body); got != Captcha {
		t.Fatalf("got %q", got)
	}
}

func TestClassifyCaptchaByRedirect(t *testing.T) {
	if got := Classify(200, "https://wappass.baidu.com/static/captcha/", "", []byte("ok")); got != Captcha {
		t.Fatalf("got %q", got)
	}
}

func TestClassifyRateLimited(t *testing.T) {
	if got := Classify(429, "https://www.baidu.com/s", "", nil); got != RateLimited {
		t.Fatalf("got %q", got)
	}
}

func TestClassifyNormalAndChanged(t *testing.T) {
	if got := Classify(200, "https://www.baidu.com/s", "go_百度搜索", []byte(`<div id="content_left"><div class="result"></div></div>`)); got != Normal {
		t.Fatalf("normal got %q", got)
	}
	if got := Classify(200, "https://www.baidu.com/s", "", []byte(`<html>unknown shell</html>`)); got != ParseChanged {
		t.Fatalf("changed got %q", got)
	}
}
