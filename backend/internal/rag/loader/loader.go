// Package loader converts supported uploaded document bytes to page-oriented text.
// PDF keeps source page numbers, DOCX reads document.xml, and plain formats use
// synthetic pages for citation offsets. HTML currently follows the plain-text path;
// this loader does not perform OCR or a browser-based extraction pass.
package loader

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	pdf "github.com/dslipak/pdf"
)

// LoadedDoc contains pages in source order for the splitter and indexer.
type LoadedDoc struct {
	Pages []PageContent
}

// PageContent associates extracted text with a one-based source or synthetic page.
// The indexer stores this number in chunk metadata for later citations.
type PageContent struct {
	Page    int
	Content string
}

// Load selects a parser from the filename extension. Unknown extensions fail
// explicitly; a missing extension is treated as plain text.
func Load(filename string, data []byte) (*LoadedDoc, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf":
		return loadPDF(data)
	case ".docx":
		return loadDocx(data)
	case ".html", ".htm":
		return loadPlainText(data), nil
	case ".md", ".markdown", ".txt", "":
		return loadPlainText(data), nil
	default:
		return nil, fmt.Errorf("unsupported file type: %s", filepath.Ext(filename))
	}
}

// loadPlainText normalizes line endings and assigns synthetic pages in blocks of
// 1,024 runes. Rune boundaries preserve Unicode characters; empty input still
// produces one page so callers receive a consistent document structure.
func loadPlainText(data []byte) *LoadedDoc {
	text := normalizeText(string(data))
	var pages []PageContent
	const chunk = 1024
	for i, r := 0, []rune(text); i < len(r); i += chunk {
		end := i + chunk
		if end > len(r) {
			end = len(r)
		}
		pages = append(pages, PageContent{
			Page:    len(pages) + 1,
			Content: string(r[i:end]),
		})
	}
	if len(pages) == 0 {
		pages = append(pages, PageContent{Page: 1, Content: text})
	}
	return &LoadedDoc{Pages: pages}
}

// loadPDF extracts readable text page by page. Unreadable or blank pages are
// skipped; a document with no extractable text fails instead of yielding an
// apparently successful empty index.
func loadPDF(data []byte) (*LoadedDoc, error) {
	reader := bytes.NewReader(data)
	doc, err := pdf.NewReader(reader, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}

	var pages []PageContent
	for i := 0; i < doc.NumPage(); i++ {
		p := doc.Page(i + 1)
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		content := normalizeText(string(text))
		if strings.TrimSpace(content) == "" {
			continue
		}
		pages = append(pages, PageContent{Page: i + 1, Content: content})
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("no text extracted from pdf")
	}
	return &LoadedDoc{Pages: pages}, nil
}

// loadDocx opens the DOCX ZIP container, reads word/document.xml, removes XML tags,
// and reuses plain-text pagination. Other archive entries are not indexed.
func loadDocx(data []byte) (*LoadedDoc, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		xmlData, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}
		return loadPlainText([]byte(stripXML(string(xmlData)))), nil
	}
	return nil, fmt.Errorf("word/document.xml not found")
}

var xmlTagRe = regexp.MustCompile(`<[^>]+>`)

// stripXML replaces markup with spaces, retaining the text between tags.
func stripXML(s string) string {
	return xmlTagRe.ReplaceAllString(s, " ")
}

// normalizeText converts CRLF/CR to LF and collapses runs of more than two newlines
// without otherwise rewriting document text.
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}
