package quality

import (
	"os"
	"strings"
	"testing"

	"web-search-backend/webfetch/internal/domain"
)

func TestArticleQualityEvaluatorDecisions(t *testing.T) {
	t.Parallel()

	evaluator := NewArticleQualityEvaluator(200)
	tests := []struct {
		name           string
		fixture        string
		sourceType     domain.SourceType
		title          string
		content        string
		wantAction     domain.QualityAction
		classification domain.ReadClassification
	}{
		{name: "article", fixture: "article.html", sourceType: domain.SourceTypeHTML, title: "文章标题", content: strings.Repeat("有效正文。", 60), wantAction: domain.QualityActionAccept, classification: domain.ReadClassificationSuccess},
		{name: "article with recaptcha script", sourceType: domain.SourceTypeHTML, title: "文章标题", content: strings.Repeat("有效正文。", 60), wantAction: domain.QualityActionAccept, classification: domain.ReadClassificationSuccess},
		{name: "js shell", fixture: "js-shell.html", sourceType: domain.SourceTypeHTML, wantAction: domain.QualityActionRender, classification: domain.ReadClassificationJSShell},
		{name: "captcha", fixture: "captcha.html", sourceType: domain.SourceTypeHTML, content: "请完成安全验证，检测到异常访问，请完成验证码后继续访问。", wantAction: domain.QualityActionReject, classification: domain.ReadClassificationCaptcha},
		{name: "login", fixture: "login.html", sourceType: domain.SourceTypeHTML, content: "请登录，登录后才能查看完整内容，请输入账号和密码。", wantAction: domain.QualityActionReject, classification: domain.ReadClassificationLoginRequired},
		{name: "plain text", sourceType: domain.SourceTypePlainText, content: "short but valid", wantAction: domain.QualityActionAccept, classification: domain.ReadClassificationSuccess},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var body []byte
			if test.fixture != "" {
				var err error
				body, err = os.ReadFile("../../../testdata/fetch/" + test.fixture)
				if err != nil {
					t.Fatalf("ReadFile() error = %v", err)
				}
			}
			if test.name == "article with recaptcha script" {
				body = []byte(`<html><body><article>valid</article><script src="https://example.com/recaptcha.js"></script></body></html>`)
			}
			result := evaluator.Evaluate(domain.ReadDocument{
				SourceType:  test.sourceType,
				Title:       test.title,
				ContentText: test.content,
			}, domain.Resource{Body: body})
			if result.Action != test.wantAction || result.Classification != test.classification {
				t.Fatalf("Evaluate() = %#v, want action %q classification %q", result, test.wantAction, test.classification)
			}
		})
	}
}
