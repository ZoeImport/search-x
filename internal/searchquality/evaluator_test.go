package searchquality

import (
	"math"
	"testing"

	"web-search-backend/internal/domain"
)

func TestEvaluatorScoresChineseIntentAboveUnrelatedResult(t *testing.T) {
	evaluator := NewEvaluator(DefaultConfig())
	good := evaluator.Evaluate("Go 语言并发模型", []domain.SearchResult{{
		Title: "Go 语言并发模型详解", URL: "https://go.dev/doc/effective_go", Snippet: "介绍 goroutine、channel 与并发编程",
	}})
	bad := evaluator.Evaluate("Go 语言并发模型", []domain.SearchResult{{
		Title: "Python 安装教程", URL: "https://example.com/python", Snippet: "介绍 pip 和虚拟环境",
	}})
	if good.Score <= bad.Score {
		t.Fatalf("good score %f <= bad score %f", good.Score, bad.Score)
	}
	if len(good.Items) != 1 || good.Items[0].Score < 0.5 {
		t.Fatalf("good result was not recognized: %+v", good)
	}
	if bad.Items[0].Score >= 0.2 {
		t.Fatalf("unrelated result scored too highly: %+v", bad)
	}
}

func TestEvaluatorScoresEnglishIntent(t *testing.T) {
	evaluator := NewEvaluator(DefaultConfig())
	result := evaluator.Evaluate("golang context cancellation", []domain.SearchResult{{
		Rank: 1, Title: "Golang context cancellation", URL: "https://go.dev/doc/database/cancel-operations", Snippet: "Use context.Context cancellation in Go",
	}})
	if result.Items[0].OriginalRank != 1 || result.Items[0].Score < 0.45 {
		t.Fatalf("unexpected item score: %+v", result.Items[0])
	}
}

func TestEvaluatorPenalizesDuplicateURLAndIncompleteFields(t *testing.T) {
	evaluator := NewEvaluator(DefaultConfig())
	result := evaluator.Evaluate("golang", []domain.SearchResult{
		{Rank: 1, Title: "Golang guide", URL: "https://example.com/a", Snippet: "golang tutorial"},
		{Rank: 2, Title: "Golang copy", URL: "https://EXAMPLE.com:443/a#section"},
	})
	if math.Abs(result.UniqueDomainRatio-0.5) > 0.0001 {
		t.Fatalf("unique domain ratio = %f", result.UniqueDomainRatio)
	}
	if result.UniqueDomainCount != 1 {
		t.Fatalf("unique domain count = %d", result.UniqueDomainCount)
	}
	if result.Completeness >= 1 {
		t.Fatalf("incomplete result set reported complete: %f", result.Completeness)
	}
	if result.Items[1].Score >= result.Items[0].Score {
		t.Fatalf("duplicate incomplete result was not penalized: %+v", result.Items)
	}
}

func TestEvaluatorReturnsZeroForEmptyInputs(t *testing.T) {
	evaluator := NewEvaluator(DefaultConfig())
	if result := evaluator.Evaluate("", nil); result.Score != 0 || len(result.Items) != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
