// Package splitter divides loaded page text into overlapping chunks. Markdown
// heading boundaries are considered first, followed by prioritized separators;
// rune counts keep chunk sizes independent of UTF-8 byte width.
package splitter

import (
	"strings"
)

// Splitter stores the target chunk size and retained trailing overlap, in runes.
// Configuration is normalized by New before splitting begins.
type Splitter struct {
	ChunkSize    int
	ChunkOverlap int
}

// New defaults non-positive sizes to 512 and replaces invalid overlap values
// with one eighth of the chunk size.
func New(size, overlap int) *Splitter {
	if size <= 0 {
		size = 512
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	return &Splitter{ChunkSize: size, ChunkOverlap: overlap}
}

// Chunk is an ordered fragment with its originating page number. Seq is assigned
// within each call to Split before records are assembled by the indexer.
type Chunk struct {
	Seq     int
	Content string
	Page    int
}

// Split separates heading sections, splits each section, and numbers the final
// fragments in output order. All fragments retain the supplied page number.
func (s *Splitter) Split(content string, page int) []Chunk {
	var chunks []Chunk
	for _, seg := range splitByHeading(content) {
		chunks = append(chunks, s.recursiveSplit(seg, page)...)
	}

	for i := range chunks {
		chunks[i].Seq = i
	}
	return chunks
}

// splitByHeading starts a new section at lines beginning with # when there is
// already text buffered, keeping the heading with the following section.
func splitByHeading(content string) []string {
	lines := strings.Split(content, "\n")
	var segs []string
	var cur strings.Builder
	for _, line := range lines {
		if strings.HasPrefix(line, "#") && cur.Len() > 0 {
			segs = append(segs, cur.String())
			cur.Reset()
		}
		cur.WriteString(line + "\n")
	}
	if cur.Len() > 0 {
		segs = append(segs, cur.String())
	}
	return segs
}

// separators is ordered from paragraph boundaries to smaller textual boundaries.
// The empty separator leaves the hard rune-splitting fallback available.
var separators = []string{"\n\n", "\n", "。", ".", " ", ""}

// recursiveSplit tries the first applicable separator, recursively handles large
// segments, and packs the remaining pieces into a chunk buffer. Completed buffers
// seed the next buffer with a trailing overlap to retain neighboring context.
func (s *Splitter) recursiveSplit(text string, page int) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len([]rune(text)) <= s.ChunkSize {
		return []Chunk{{Content: text, Page: page}}
	}

	for _, sep := range separators {
		if !strings.Contains(text, sep) {
			continue
		}
		parts := splitKeepSep(text, sep)
		var chunks []Chunk
		var buf strings.Builder

		for _, p := range parts {
			if len([]rune(p)) > s.ChunkSize && sep != "" {
				chunks = append(chunks, s.recursiveSplit(p, page)...)
				continue
			}

			if len([]rune(buf.String()+p)) > s.ChunkSize && buf.Len() > 0 {
				chunks = append(chunks, Chunk{Content: strings.TrimSpace(buf.String()), Page: page})

				// Copy the previous buffer tail before resetting it; overlap is measured in
				// characters, not bytes.
				tail := []rune(buf.String())
				if len(tail) > s.ChunkOverlap {
					tail = tail[len(tail)-s.ChunkOverlap:]
				}
				buf.Reset()
				buf.WriteString(string(tail))
			}
			buf.WriteString(p)
		}
		if strings.TrimSpace(buf.String()) != "" {
			chunks = append(chunks, Chunk{Content: strings.TrimSpace(buf.String()), Page: page})
		}
		if len(chunks) > 0 {
			return chunks
		}
	}

	// If separator-based packing produced no chunks, split directly by runes with
	// the configured overlap and stop after the final fragment.
	r := []rune(text)
	var chunks []Chunk
	for i := 0; i < len(r); i += s.ChunkSize - s.ChunkOverlap {
		end := i + s.ChunkSize
		if end > len(r) {
			end = len(r)
		}
		chunks = append(chunks, Chunk{Content: string(r[i:end]), Page: page})
		if end == len(r) {
			break
		}
	}
	return chunks
}

// splitKeepSep retains each delimiter at the end of its segment so separators
// are not lost when adjacent pieces are reassembled into chunks.
func splitKeepSep(text, sep string) []string {
	if sep == "" {
		return []string{text}
	}
	parts := strings.Split(text, sep)
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i < len(parts)-1 {
			p += sep
		}
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}
