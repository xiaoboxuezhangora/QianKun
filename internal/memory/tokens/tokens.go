package tokens

import "unicode"

// EstimateTextTokens returns a conservative, tokenizer-free estimate for W2.
// ASCII-like text is estimated at roughly 4 non-space characters per token.
// CJK text is weighted higher because it is usually denser per rune.
func EstimateTextTokens(text string) int {
	var asciiChars int
	var cjkRunes int
	var otherRunes int

	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		if isCJK(r) {
			cjkRunes++
			continue
		}
		if r <= unicode.MaxASCII {
			asciiChars++
			continue
		}
		otherRunes++
	}

	if asciiChars == 0 && cjkRunes == 0 && otherRunes == 0 {
		return 0
	}

	asciiTokens := ceilDiv(asciiChars, 4)
	cjkTokens := ceilDiv(cjkRunes*3, 2)
	otherTokens := ceilDiv(otherRunes*2, 3)

	total := asciiTokens + cjkTokens + otherTokens
	if total < 1 {
		return 1
	}
	return total
}

func ceilDiv(n, d int) int {
	if n == 0 {
		return 0
	}
	return (n + d - 1) / d
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}
