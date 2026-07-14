package detector

import (
	"bytes"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"web-search-backend/internal/domain"
)

// Classification is the typed outcome of an upstream search attempt.
type Classification = domain.Classification

const (
	// Normal indicates a normal result page.
	Normal Classification = domain.ClassificationNormal
	// Empty indicates a valid page without results.
	Empty Classification = domain.ClassificationEmpty
	// Captcha indicates a security verification page.
	Captcha Classification = domain.ClassificationCaptcha
	// RateLimited indicates an upstream rate limit.
	RateLimited Classification = domain.ClassificationRateLimited
	// Blocked indicates an upstream access block.
	Blocked Classification = domain.ClassificationBlocked
	// ParseChanged indicates unexpected upstream markup.
	ParseChanged Classification = domain.ClassificationParseChanged
	// Timeout indicates a context or upstream timeout.
	Timeout Classification = domain.ClassificationTimeout
	// NetworkError indicates a transport-level network error.
	NetworkError Classification = domain.ClassificationNetworkError
)

// Classify identifies the outcome represented by a Baidu HTTP response.
func Classify(status int, finalURL, pageTitle string, body []byte) Classification {
	if status == 429 {
		return RateLimited
	}
	if status == 403 || status == 503 || status >= 500 {
		return Blocked
	}

	lowerBody := strings.ToLower(string(body))
	if pageTitle == "" {
		pageTitle = ExtractTitle(body)
	}
	if captchaURL(finalURL) || strings.Contains(pageTitle, "百度安全验证") || captchaBody(lowerBody) {
		return Captcha
	}
	if status >= 400 {
		return Blocked
	}
	if hasNormalRoot(lowerBody) && hasResultCard(lowerBody) {
		return Normal
	}
	if hasNormalRoot(lowerBody) && hasEmptyMarker(lowerBody) {
		return Empty
	}
	return ParseChanged
}

// ExtractTitle returns the normalized text of the first HTML title element.
func ExtractTitle(body []byte) string {
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return ""
		case html.StartTagToken:
			token := tokenizer.Token()
			if strings.EqualFold(token.Data, "title") && tokenizer.Next() == html.TextToken {
				return strings.Join(strings.Fields(tokenizer.Token().Data), " ")
			}
		}
	}
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
		strings.Contains(body, "id='verify-form'")
	challengeText := strings.Contains(body, "请输入验证码") ||
		strings.Contains(body, "百度安全验证") ||
		strings.Contains(body, "security verification")
	return strongForm || (challengeText && strings.Contains(body, "<form"))
}

func hasNormalRoot(body string) bool {
	return strings.Contains(body, "id=\"content_left\"") ||
		strings.Contains(body, "id='content_left'") ||
		strings.Contains(body, "id=\"results\"") ||
		strings.Contains(body, "id='results'")
}

func hasResultCard(body string) bool {
	return strings.Contains(body, "class=\"result") ||
		strings.Contains(body, "class='result") ||
		strings.Contains(body, "class=\"c-container") ||
		strings.Contains(body, "class='c-container") ||
		strings.Contains(body, "data-rank=") ||
		strings.Contains(body, "data-log=")
}

func hasEmptyMarker(body string) bool {
	return strings.Contains(body, "class=\"nors") ||
		strings.Contains(body, "class='nors") ||
		strings.Contains(body, "没有找到相关结果") ||
		strings.Contains(body, "未找到相关结果")
}
