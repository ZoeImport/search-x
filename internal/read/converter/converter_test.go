package converter

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"web-search-backend/internal/domain"
)

func TestMarkdownConverterPreservesStructure(t *testing.T) {
	t.Parallel()

	formatted, err := (MarkdownConverter{}).Convert(context.Background(), domain.ReadDocument{
		ContentHTML: `<h1>标题</h1><p>正文 <strong>重点</strong> <a href="https://example.com">链接</a></p><ul><li>项目一</li><li>项目二</li></ul>`,
	})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	for _, want := range []string{"# 标题", "**重点**", "[链接](https://example.com)", "- 项目一"} {
		if !strings.Contains(formatted.Content, want) {
			t.Fatalf("Content = %q, want substring %q", formatted.Content, want)
		}
	}
}

func TestTruncateContentUsesParagraphAndRuneBoundaries(t *testing.T) {
	t.Parallel()

	content := "第一段中文\n\n第二段中文较长\n\n第三段"
	truncated, wasTruncated := TruncateContent(content, 12)
	if !wasTruncated {
		t.Fatal("TruncateContent() wasTruncated = false")
	}
	if truncated != "第一段中文" {
		t.Fatalf("TruncateContent() = %q, want first paragraph", truncated)
	}
	if !utf8.ValidString(truncated) || utf8.RuneCountInString(truncated) > 12 {
		t.Fatalf("invalid rune-safe result %q", truncated)
	}

	fallback, _ := TruncateContent("你好世界和平", 3)
	if fallback != "你好世" {
		t.Fatalf("rune fallback = %q, want %q", fallback, "你好世")
	}
}

func TestTextConverterUsesCanonicalText(t *testing.T) {
	t.Parallel()

	formatted, err := (TextConverter{}).Convert(context.Background(), domain.ReadDocument{ContentText: "正文内容"})
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	if formatted.Content != "正文内容" || formatted.Format != domain.OutputFormatText {
		t.Fatalf("formatted = %#v", formatted)
	}
}
