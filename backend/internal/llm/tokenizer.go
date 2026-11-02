package llm

import "strings"

// EstimateTokens provides a coarse text-size estimate for document metadata. It
// counts CJK-range runes, whitespace-separated words, and a fraction of remaining
// characters; provider-reported usage remains authoritative for model billing.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Keep the estimate inexpensive and independent of a model-specific tokenizer.
	cjk, ascii := 0, 0
	for _, r := range text {
		if r > 0x2E80 {
			cjk++
		} else {
			ascii++
		}
	}
	words := len(strings.Fields(text))
	return cjk + words*13/10 + ascii/4
}
