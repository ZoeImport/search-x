package searchplan

import (
	"strings"

	"web-search-backend/websearch/internal/domain"
)

type compiler interface {
	Supports(domain.SearchQueryOptions) bool
	Compile(domain.SearchRequest) string
}

type domainOnlyCompiler struct{}

func (domainOnlyCompiler) Supports(options domain.SearchQueryOptions) bool {
	return !hasQueryOptions(options)
}

func (domainOnlyCompiler) Compile(request domain.SearchRequest) string {
	return appendDomainOperators([]string{request.Query}, request.Filters)
}

type braveCompiler struct{}

func (braveCompiler) Supports(domain.SearchQueryOptions) bool { return true }

func (braveCompiler) Compile(request domain.SearchRequest) string {
	return compileDocumentedOperators(request, "NOT site:")
}

type duckDuckGoCompiler struct{}

func (duckDuckGoCompiler) Supports(options domain.SearchQueryOptions) bool {
	if len(options.FileTypes) > 1 {
		return false
	}
	for _, value := range options.FileTypes {
		switch value {
		case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "html":
		default:
			return false
		}
	}
	return true
}

func (duckDuckGoCompiler) Compile(request domain.SearchRequest) string {
	return compileDocumentedOperators(request, "-site:")
}

func compilerFor(provider domain.ProviderName) compiler {
	switch provider {
	case domain.ProviderNameBrave:
		return braveCompiler{}
	case domain.ProviderNameDuckDuckGo:
		return duckDuckGoCompiler{}
	default:
		return domainOnlyCompiler{}
	}
}

func compileDocumentedOperators(request domain.SearchRequest, excludeSitePrefix string) string {
	parts := []string{request.Query}
	for _, value := range request.QueryOptions.ExactPhrases {
		parts = append(parts, quote(value))
	}
	if len(request.QueryOptions.AnyTerms) > 0 {
		alternatives := make([]string, len(request.QueryOptions.AnyTerms))
		for index, value := range request.QueryOptions.AnyTerms {
			alternatives[index] = quoteWhenNeeded(value)
		}
		parts = append(parts, "("+strings.Join(alternatives, " OR ")+")")
	}
	for _, value := range request.QueryOptions.ExcludeTerms {
		parts = append(parts, "-"+quoteWhenNeeded(value))
	}
	for _, value := range request.QueryOptions.TitleTerms {
		parts = append(parts, "intitle:"+quoteWhenNeeded(value))
	}
	if len(request.QueryOptions.FileTypes) == 1 {
		parts = append(parts, "filetype:"+request.QueryOptions.FileTypes[0])
	} else if len(request.QueryOptions.FileTypes) > 1 {
		values := make([]string, len(request.QueryOptions.FileTypes))
		for index, value := range request.QueryOptions.FileTypes {
			values[index] = "filetype:" + value
		}
		parts = append(parts, "("+strings.Join(values, " OR ")+")")
	}
	return appendDomainOperatorsWithExclude(parts, request.Filters, excludeSitePrefix)
}

func appendDomainOperators(parts []string, filters domain.SearchFilters) string {
	return appendDomainOperatorsWithExclude(parts, filters, "-site:")
}

func appendDomainOperatorsWithExclude(parts []string, filters domain.SearchFilters, excludePrefix string) string {
	if len(filters.IncludeDomains) == 1 {
		parts = append(parts, "site:"+filters.IncludeDomains[0])
	} else if len(filters.IncludeDomains) > 1 {
		included := make([]string, len(filters.IncludeDomains))
		for index, value := range filters.IncludeDomains {
			included[index] = "site:" + value
		}
		parts = append(parts, "("+strings.Join(included, " OR ")+")")
	}
	for _, value := range filters.ExcludeDomains {
		parts = append(parts, excludePrefix+value)
	}
	return strings.Join(parts, " ")
}
