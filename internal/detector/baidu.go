package detector

import (
	"net/url"
	"strings"
)

type Classification string

const (
	Normal       Classification = "normal"
	Empty        Classification = "empty"
	Captcha      Classification = "captcha"
	RateLimited  Classification = "rate_limited"
	Blocked      Classification = "blocked"
	ParseChanged Classification = "parse_changed"
	Timeout      Classification = "timeout"
	NetworkError Classification = "network_error"
)

func Classify(status int, finalURL string, body []byte) Classification {
	if status == 429 {
		return RateLimited
	}
	if status == 403 || status == 503 || status >= 500 {
		return Blocked
	}

	lowerBody := strings.ToLower(string(body))
	if captchaURL(finalURL) || captchaBody(lowerBody) {
		return Captcha
	}
	if status >= 400 {
		return Blocked
	}
	if hasNormalRoot(lowerBody) {
		return Normal
	}
	return ParseChanged
}

func captchaURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	path := strings.ToLower(u.Path)
	return host == "wappass.baidu.com" ||
		(strings.HasSuffix(host, ".baidu.com") && (strings.Contains(path, "captcha") || strings.Contains(path, "verify")))
}

func captchaBody(body string) bool {
	strongForm := strings.Contains(body, "id=\"verify-form\"") ||
		strings.Contains(body, "id='verify-form'") ||
		strings.Contains(body, "class=\"captcha")
	challengeText := strings.Contains(body, "请输入验证码") ||
		strings.Contains(body, "安全验证") ||
		strings.Contains(body, "security verification")
	return strongForm || (challengeText && strings.Contains(body, "<form"))
}

func hasNormalRoot(body string) bool {
	return strings.Contains(body, "id=\"content_left\"") ||
		strings.Contains(body, "id='content_left'") ||
		strings.Contains(body, "id=\"results\"") ||
		strings.Contains(body, "id='results'")
}
