package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SearchConfig separates the search backend from direct URL fetching. HTTPClient
// is shared by both handlers; DomainWhitelist applies only to http_client.
type SearchConfig struct {
	SearxngURL string
	HTTPClient *http.Client

	// DomainWhitelist permits exact hosts and subdomains. An empty list rejects
	// all direct fetch destinations.
	DomainWhitelist []string
}

// RegisterWeb installs web_search and http_client. When no client is injected,
// both share a ten-second overall HTTP timeout in addition to tool cancellation.
func RegisterWeb(r Registry, cfg SearchConfig) {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	r.Register("web_search", func(ctx context.Context, args map[string]any, _ string) (string, error) {
		return runWebSearch(ctx, cfg, args)
	})
	r.Register("http_client", func(ctx context.Context, args map[string]any, _ string) (string, error) {
		return runHTTPClient(ctx, cfg, args)
	})
}

// runWebSearch queries the configured SearXNG JSON endpoint and returns a compact
// list of titles, URLs, and snippets. count defaults to five and accepts values
// from one through twenty; response bytes and snippet runes are bounded.
func runWebSearch(ctx context.Context, cfg SearchConfig, args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return "", fmt.Errorf("query is required")
	}
	count := 5
	if v, ok := args["count"].(float64); ok && int(v) > 0 && int(v) <= 20 {
		count = int(v)
	}

	params := url.Values{
		"q":        {query},
		"format":   {"json"},
		"language": {"en"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(cfg.SearxngURL, "/")+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return "", err
	}

	var sr struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &sr); err != nil {
		return "", fmt.Errorf("parse search result: %v", err)
	}

	out := make([]map[string]string, 0, count)
	for i, r := range sr.Results {
		if i >= count {
			break
		}
		out = append(out, map[string]string{
			"title": r.Title, "url": r.URL, "snippet": truncateText(r.Content, 200),
		})
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}

// runHTTPClient fetches an HTTP(S) URL after checking its initial hostname against
// the configured whitelist. It forwards optional headers, bounds the response
// body, removes HTML tags, and returns status plus a short plain-text excerpt.
func runHTTPClient(ctx context.Context, cfg SearchConfig, args map[string]any) (string, error) {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("invalid url")
	}

	if !domainAllowed(u.Hostname(), cfg.DomainWhitelist) {
		return "", fmt.Errorf("domain %s not in whitelist", u.Hostname())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	if h, ok := args["headers"].(map[string]any); ok {
		for k, v := range h {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))

	text := stripHTML(string(body))
	return fmt.Sprintf(`{"status": %d, "content": %q}`, resp.StatusCode, truncateText(text, 4000)), nil
}

// domainAllowed matches a configured host or its dot-delimited subdomains. It
// does not treat an arbitrary string suffix as a matching domain.
func domainAllowed(host string, whitelist []string) bool {
	for _, w := range whitelist {
		if host == w || strings.HasSuffix(host, "."+w) {
			return true
		}
	}
	return false
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>\s*`)

// stripHTML removes tag-like markup and normalizes whitespace for model context.
// It is text cleanup, not a full HTML parser or browser renderer.
func stripHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// truncateText limits excerpts by rune count so multilingual snippets remain valid
// UTF-8, appending an ellipsis when text is shortened.
func truncateText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
