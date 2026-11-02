// Package pagination parses bounded list parameters shared by API and repository calls.
// The result contains page-based navigation plus an optional keyword; resource
// ownership and filtering remain the responsibility of the calling service.
package pagination

import (
	"net/http"
	"strconv"
)

// Query is the normalized page request. Page starts at one, PageSize is capped,
// and Keyword is passed through without choosing a database search strategy.
type Query struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Keyword  string `json:"keyword"`
}

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// Parse reads page, page_size, and keyword from HTTP query parameters.
// Missing or invalid numbers use defaults; pages below one and sizes outside
// 1..100 are reset to page one and 20 records respectively.
func Parse(r *http.Request) Query {
	q := Query{
		Page:     toInt(r.URL.Query().Get("page"), defaultPage),
		PageSize: toInt(r.URL.Query().Get("page_size"), defaultPageSize),
		Keyword:  string(r.URL.Query().Get("keyword")),
	}
	if q.Page < 1 {
		q.Page = defaultPage
	}
	if q.PageSize < 1 || q.PageSize > maxPageSize {
		q.PageSize = defaultPageSize
	}
	return q
}

// Offset converts the normalized one-based page into a zero-based SQL row offset.
func (q Query) Offset() int { return (q.Page - 1) * q.PageSize }

// Limit returns the validated page size for repository queries.
func (q Query) Limit() int { return q.PageSize }

// toInt returns the fallback for an absent or noninteger query parameter.
func toInt(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
