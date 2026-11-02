package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"gorm.io/datatypes"

	"github.com/Emulisy/SupaAgent/internal/model"
	"github.com/Emulisy/SupaAgent/internal/vector"
)

// FusedChunk retains the source record and filename while replacing raw dense or
// keyword scores with a fused rank score, or later a reranker relevance score.
type FusedChunk struct {
	Chunk    model.DocumentChunk
	Filename string
	Score    float32
}

// Fuse combines dense and keyword rankings by chunk ID using reciprocal rank
// fusion: each occurrence contributes 1/(k + rank), with ranks starting at one.
// This avoids comparing cosine similarity directly with keyword scores. The
// merged results are sorted descending and optionally truncated to topN.
func Fuse(denseHits []vector.SearchHit, kwHits []ScoredChunk, k float64, topN int) []FusedChunk {
	type entry struct {
		chunk    model.DocumentChunk
		filename string
		score    float64
	}

	// Keep one entry per chunk so a candidate present in both retrieval paths
	// receives both rank contributions instead of appearing twice.
	byID := map[int64]*entry{}

	rank := func(i int) float64 { return 1.0 / (k + float64(i+1)) }
	for _, h := range denseHits {
		byID[h.ID] = &entry{chunk: chunkFromHit(h)}
	}

	for idx, h := range denseHits {
		if e, ok := byID[h.ID]; ok {
			e.score += rank(idx)
		}
	}

	// Keyword records can supply filenames that are absent from dense ID hits.
	for i, h := range kwHits {
		if e, ok := byID[h.Chunk.ID]; ok {
			e.filename = h.Filename
			e.score += rank(i)
		} else {
			byID[h.Chunk.ID] = &entry{chunk: h.Chunk, filename: h.Filename, score: rank(i)}
		}
	}

	out := make([]FusedChunk, 0, len(byID))
	for _, e := range byID {
		out = append(out, FusedChunk{Chunk: e.chunk, Filename: e.filename, Score: float32(e.score)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}

// chunkFromHit restores any metadata available on a vector hit. The current
// Milvus search path supplies only the ID, so Retrieve later hydrates missing text
// and citation fields from PostgreSQL.
func chunkFromHit(h vector.SearchHit) model.DocumentChunk {
	c := model.DocumentChunk{ID: h.ID}
	if h.Metadata != nil {
		if v, ok := h.Metadata["doc_id"].(int64); ok {
			c.DocumentID = v
		}
		if v, ok := h.Metadata["seq"].(int32); ok {
			c.Seq = int(v)
		}
		if v, ok := h.Metadata["page"].(int32); ok {
			c.Meta = datatypes.JSON(fmt.Sprintf(`{"page":%d}`, v))
		}
		if v, ok := h.Metadata["content"].(string); ok {
			c.Content = v
		}
	}
	return c
}

// chunkPage reads the page number from chunk metadata and returns zero when it
// is absent or malformed.
func chunkPage(c model.DocumentChunk) int {
	var m struct {
		Page int `json:"page"`
	}
	if err := json.Unmarshal(c.Meta, &m); err != nil {
		return 0
	}
	return m.Page
}

// extractTerms lowercases text, keeps contiguous ASCII-range letters/digits as
// words, and emits overlapping bigrams for contiguous CJK-range characters.
// Duplicate terms are removed in first-seen order to avoid repeated score weight.
func extractTerms(query string) []string {
	query = strings.ToLower(query)
	var terms []string
	var word strings.Builder
	var cjk strings.Builder

	flushWord := func() {
		if word.Len() > 0 {
			terms = append(terms, word.String())
			word.Reset()
		}
	}
	// Single characters remain searchable; longer runs use adjacent pairs so
	// Chinese queries do not depend on whitespace word boundaries.
	flushCJK := func() {
		s := cjk.String()
		cjk.Reset()
		r := []rune(s)
		if len(r) == 1 {
			terms = append(terms, s)
			return
		}
		for i := 0; i+1 < len(r); i++ {
			terms = append(terms, string(r[i:i+2]))
		}
	}

	for _, ch := range query {
		switch {
		case unicode.IsLetter(ch) && ch < 0x2E80 || unicode.IsDigit(ch):
			flushCJK()
			word.WriteRune(ch)
		case ch > 0x2E80:
			flushWord()
			cjk.WriteRune(ch)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()

	// Preserve term order while deduplicating without a second output allocation.
	seen := map[string]bool{}
	uniq := terms[:0]
	for _, t := range terms {
		if !seen[t] {
			seen[t] = true
			uniq = append(uniq, t)
		}
	}
	return uniq
}
