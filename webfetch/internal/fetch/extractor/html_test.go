package extractor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"web-search-backend/webfetch/internal/domain"
)

func TestHTMLExtractorExtractsArticleAndMetadata(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile("../../../testdata/fetch/article.html")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	document, err := (HTMLExtractor{}).Extract(context.Background(), domain.Resource{
		URL:      "https://example.com/original",
		FinalURL: "https://example.com/articles/one",
		Body:     body,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if document.Title != "一篇用于测试的文章" {
		t.Fatalf("Title = %q", document.Title)
	}
	if document.Author != "测试作者" || document.Language != "zh-CN" {
		t.Fatalf("metadata = author %q, language %q", document.Author, document.Language)
	}
	if !strings.Contains(document.ContentHTML, `href="https://example.com/reference"`) {
		t.Fatalf("ContentHTML does not contain absolute link: %s", document.ContentHTML)
	}
	for _, unwanted := range []string{"首页 产品 登录", "should-not-appear", "<script"} {
		if strings.Contains(document.ContentHTML+document.ContentText, unwanted) {
			t.Fatalf("extracted content contains %q", unwanted)
		}
	}
}

func TestHTMLExtractorRejectsMalformedDocument(t *testing.T) {
	t.Parallel()

	_, err := (HTMLExtractor{}).Extract(context.Background(), domain.Resource{Body: nil})
	if err == nil {
		t.Fatal("Extract() error = nil, want non-nil")
	}
}

func TestHTMLExtractorSelectsLargestArticleCandidate(t *testing.T) {
	t.Parallel()

	body := []byte(`<html><body>
		<article><h1>推荐摘要</h1><p>很短的推荐。</p></article>
		<main><h1>完整文章</h1><p>这是更长的主要正文段落，应该被提取器选中。</p><p>第二段进一步证明这是页面的主要文章，而不是推荐卡片。</p></main>
	</body></html>`)
	document, err := (HTMLExtractor{}).Extract(context.Background(), domain.Resource{Body: body})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if document.Title != "完整文章" || strings.Contains(document.ContentText, "很短的推荐") {
		t.Fatalf("selected document = %#v", document)
	}
}

func TestHTMLExtractorRemovesUnsafeLinkAndImageSchemes(t *testing.T) {
	body := []byte(`<html><body><article><h1>安全正文</h1>
		<p><a href="file:///etc/passwd">file</a><a href="javascript:alert(1)">js</a></p>
		<img src="data:text/plain,secret"><img src="https://example.com/image.png">
	</article></body></html>`)
	document, err := (HTMLExtractor{}).Extract(context.Background(), domain.Resource{
		URL: "https://example.com/article", FinalURL: "https://example.com/article", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(document.ContentHTML, "file:") || strings.Contains(document.ContentHTML, "javascript:") || strings.Contains(document.ContentHTML, "data:") {
		t.Fatalf("unsafe URL survived: %s", document.ContentHTML)
	}
	if !strings.Contains(document.ContentHTML, "https://example.com/image.png") {
		t.Fatalf("safe URL removed: %s", document.ContentHTML)
	}
}

func TestDOMArticleStrategyExtractsKnownArticleContainers(t *testing.T) {
	body := []byte(`<html lang="zh"><head><title>腾讯云文章标题</title><meta name="author" content="测试作者"></head><body>
		<div class="cdc-article-page"><div class="mod-article-content"><div class="mod-content"><div class="mod-content__markdown">
		<h1>初识 Go 语言</h1>
		<p>` + strings.Repeat("这是一段用于验证 DOM 正文 fallback 的有效中文内容。", 30) + `</p>
		<p><a href="/developer/reference">参考资料</a></p>
		</div></div></div></div><script>window.__NEXT_DATA__={}</script></body></html>`)

	document, err := (DOMArticleStrategy{}).Extract(context.Background(), domain.Resource{
		URL: "https://cloud.tencent.com/developer/article/2411607", FinalURL: "https://cloud.tencent.com/developer/article/2411607", Body: body,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if document.Title != "初识 Go 语言" || !strings.Contains(document.ContentText, "DOM 正文 fallback") {
		t.Fatalf("document = %#v", document)
	}
	if !strings.Contains(document.ContentHTML, `href="https://cloud.tencent.com/developer/reference"`) {
		t.Fatalf("ContentHTML does not contain absolute link: %s", document.ContentHTML)
	}
}

func TestHTMLExtractorUsesNextStrategyAfterPrimaryFailure(t *testing.T) {
	extractor, err := NewHTMLExtractor(
		stubHTMLStrategy{name: domain.ImplementationNameReadabilityExtractor, err: fmt.Errorf("readability returned no node")},
		stubHTMLStrategy{name: domain.ImplementationNameDOMArticleExtractor, document: domain.ReadDocument{Title: "fallback", ContentText: "body"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	document, err := extractor.Extract(context.Background(), domain.Resource{Body: []byte("html")})
	if err != nil {
		t.Fatal(err)
	}
	if document.Title != "fallback" || document.Extractor != domain.ImplementationNameDOMArticleExtractor {
		t.Fatalf("document = %#v", document)
	}
}

func TestHTMLExtractorClassifiesJavaScriptChallenge(t *testing.T) {
	_, err := (HTMLExtractor{}).Extract(context.Background(), domain.Resource{
		Body: []byte(`<html><body><script>window.solveChallenge('token'); document.cookie='EO-Bot-Js-Token=x'</script></body></html>`),
	})
	var coder interface {
		ReadErrorCode() domain.ErrorCode
	}
	if !errors.As(err, &coder) || coder.ReadErrorCode() != domain.ErrCaptchaRequired {
		t.Fatalf("error = %v, want captcha_required", err)
	}
}

type stubHTMLStrategy struct {
	name     domain.ImplementationName
	document domain.ReadDocument
	err      error
}

func (strategy stubHTMLStrategy) Name() domain.ImplementationName { return strategy.name }
func (strategy stubHTMLStrategy) Extract(context.Context, domain.Resource) (domain.ReadDocument, error) {
	return strategy.document, strategy.err
}
