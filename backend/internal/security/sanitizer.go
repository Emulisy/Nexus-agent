// Input normalization removes selected invisible/control characters before the user message is persisted and sent to models.
package security

import (
	"strings"
)

// SanitizeUserInput removes zero-width space, BOM, and controls below U+0020.
// Newlines and tabs are retained so multiline content keeps its structure. Other
// Unicode characters are preserved; this function does not classify prompt injection.
func SanitizeUserInput(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\u200b' || r == '\ufeff' || (r < 0x20 && r != '\n' && r != '\t') {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
