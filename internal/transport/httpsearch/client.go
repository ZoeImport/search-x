package httpsearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"web-search-backend/internal/domain"
	"web-search-backend/internal/headerprofile"
	"web-search-backend/internal/transport"
)

const defaultMaxBodyBytes int64 = 4 << 20

type Config struct {
	Name         domain.TransportName
	BaseURL      string
	UserAgent    string
	Referer      string
	Timeout      time.Duration
	MaxBodyBytes int64
	// HeaderProfiles overrides static request headers with a sticky profile.
	HeaderProfiles headerprofile.Pool
}

type Client struct {
	config Config
	client *http.Client
}

func New(config Config, client *http.Client) (*Client, error) {
	config.Name = domain.TransportName(strings.TrimSpace(string(config.Name)))
	if config.Name == "" {
		return nil, fmt.Errorf("transport name is empty")
	}
	parsed, err := url.Parse(config.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("invalid base URL %q", config.BaseURL)
	}
	if config.Timeout <= 0 {
		return nil, fmt.Errorf("transport timeout must be positive")
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultMaxBodyBytes
	}
	if config.UserAgent == "" {
		config.UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0 Safari/537.36"
	}
	if client == nil {
		client = &http.Client{}
	}
	return &Client{config: config, client: client}, nil
}

func (c *Client) Name() domain.TransportName {
	return c.config.Name
}

func (c *Client) Fetch(ctx context.Context, request domain.SearchRequest) (transport.Response, error) {
	requestURL, err := c.buildURL(request)
	if err != nil {
		return transport.Response{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return transport.Response{RequestURL: requestURL}, fmt.Errorf("create %s request: %w", c.Name(), err)
	}
	response := transport.Response{RequestURL: requestURL}
	httpRequest.Header.Set("User-Agent", c.config.UserAgent)
	httpRequest.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	httpRequest.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.7")
	if c.config.HeaderProfiles != nil {
		profile, selectErr := c.config.HeaderProfiles.Select(profileKey(request), 0)
		if selectErr != nil {
			return response, fmt.Errorf("select %s header profile: %w", c.Name(), selectErr)
		}
		response.HeaderProfile = string(profile.Name)
		httpRequest.Header.Set("User-Agent", profile.UserAgent)
		httpRequest.Header.Set("Accept-Language", profile.AcceptLanguage)
		for key, value := range profile.Headers {
			httpRequest.Header.Set(key, value)
		}
	}
	if c.config.Referer != "" {
		httpRequest.Header.Set("Referer", c.config.Referer)
	}

	started := time.Now()
	httpResponse, err := c.client.Do(httpRequest)
	elapsed := time.Since(started)
	if err != nil {
		response.Elapsed = elapsed
		return response, fmt.Errorf("%s request: %w", c.Name(), err)
	}
	defer httpResponse.Body.Close()

	response.RequestURL = requestURL
	response.StatusCode = httpResponse.StatusCode
	response.FinalURL = httpResponse.Request.URL.String()
	response.Headers = httpResponse.Header.Clone()
	response.Elapsed = elapsed
	limited, err := io.ReadAll(io.LimitReader(httpResponse.Body, c.config.MaxBodyBytes+1))
	if err != nil {
		return response, fmt.Errorf("read %s response: %w", c.Name(), err)
	}
	if int64(len(limited)) > c.config.MaxBodyBytes {
		response.Body = limited[:c.config.MaxBodyBytes]
		return response, fmt.Errorf("%s response body exceeds %d bytes", c.Name(), c.config.MaxBodyBytes)
	}
	response.Body = limited
	return response, nil
}

func profileKey(request domain.SearchRequest) string {
	if request.RequestID != "" {
		return request.RequestID
	}
	return request.Query
}

func (c *Client) buildURL(request domain.SearchRequest) (string, error) {
	u, err := url.Parse(c.config.BaseURL)
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	values := u.Query()
	values.Set("wd", request.Query)
	values.Set("rn", strconv.Itoa(request.Limit))
	values.Set("pn", strconv.Itoa((request.Page-1)*request.Limit))
	values.Set("ie", "utf-8")
	u.RawQuery = values.Encode()
	return u.String(), nil
}
