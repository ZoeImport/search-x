// Package quality provides typed canonical content quality decisions.
package quality

import (
	"strings"
	"unicode/utf8"

	"web-search-backend/internal/domain"
)

// ArticleQualityEvaluator evaluates HTML article quality and plain-text validity.
type ArticleQualityEvaluator struct {
	minHTMLRunes int
}

// NewArticleQualityEvaluator creates an evaluator with the minimum accepted HTML text length.
func NewArticleQualityEvaluator(minHTMLRunes int) *ArticleQualityEvaluator {
	if minHTMLRunes < 1 {
		minHTMLRunes = 200
	}
	return &ArticleQualityEvaluator{minHTMLRunes: minHTMLRunes}
}

// Name returns the typed implementation name.
func (*ArticleQualityEvaluator) Name() domain.ImplementationName {
	return domain.ImplementationNameArticleQualityEvaluator
}

// Evaluate returns an explicit accept, render, or reject decision.
func (evaluator *ArticleQualityEvaluator) Evaluate(document domain.ReadDocument, resource domain.Resource) domain.QualityResult {
	content := strings.TrimSpace(document.ContentText)
	visibleContent := strings.ToLower(strings.TrimSpace(document.Title + "\n" + content))
	rawHTML := strings.ToLower(string(resource.Body))
	if containsAny(visibleContent, "验证码", "安全验证", "verify you are human", "captcha", "人机验证") {
		return domain.QualityResult{Action: domain.QualityActionReject, Classification: domain.ReadClassificationCaptcha, Reason: "verification challenge detected"}
	}
	if containsAny(visibleContent, "登录后", "请登录", "sign in to continue", "log in to continue", "账号和密码") {
		return domain.QualityResult{Action: domain.QualityActionReject, Classification: domain.ReadClassificationLoginRequired, Reason: "login-required content detected"}
	}
	if document.SourceType == domain.SourceTypePlainText {
		if content == "" {
			return domain.QualityResult{Action: domain.QualityActionReject, Classification: domain.ReadClassificationEmpty, Reason: "plain-text content is empty"}
		}
		return domain.QualityResult{Action: domain.QualityActionAccept, Classification: domain.ReadClassificationSuccess}
	}
	if looksLikeJSShell(rawHTML, content) {
		return domain.QualityResult{Action: domain.QualityActionRender, Classification: domain.ReadClassificationJSShell, Reason: "HTML appears to be a JavaScript shell"}
	}
	if utf8.RuneCountInString(strings.Join(strings.Fields(content), "")) < evaluator.minHTMLRunes {
		return domain.QualityResult{Action: domain.QualityActionRender, Classification: domain.ReadClassificationTooShort, Reason: "extracted HTML content is too short"}
	}
	if strings.TrimSpace(document.Title) == "" && countParagraphs(content) < 2 {
		return domain.QualityResult{Action: domain.QualityActionRender, Classification: domain.ReadClassificationTooShort, Reason: "content has neither title nor multiple paragraphs"}
	}
	return domain.QualityResult{Action: domain.QualityActionAccept, Classification: domain.ReadClassificationSuccess}
}

func containsAny(content string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(content, value) {
			return true
		}
	}
	return false
}

func looksLikeJSShell(rawHTML, content string) bool {
	if utf8.RuneCountInString(strings.TrimSpace(content)) >= 80 {
		return false
	}
	hasMount := containsAny(rawHTML, `id="app"`, `id="root"`, `id='app'`, `id='root'`, "__next", "ng-version")
	hasScript := strings.Contains(rawHTML, "<script")
	return hasMount && hasScript
}

func countParagraphs(content string) int {
	count := 0
	for _, paragraph := range strings.Split(content, "\n\n") {
		if strings.TrimSpace(paragraph) != "" {
			count++
		}
	}
	return count
}
