package library

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Searching ignores case and accents, so "trinh cong son" finds "Trịnh
// Công Sơn" and "beyonce" finds "Beyoncé". Letters are split into their
// base letter and accents (NFKD, which also turns full-width letters into
// ordinary ones), and the accents dropped: those of the Combining
// Diacritical Marks block, which has every Vietnamese tone mark, but not
// the marks of Japanese kana, which make different sounds. A few letters are letters of
// their own rather than accented ones, such as Vietnamese đ, so they are
// mapped by hand. Scripts without accents, such as Chinese, are unchanged.

var foldLetters = strings.NewReplacer("đ", "d", "ð", "d", "ø", "o", "ł", "l", "ß", "ss", "æ", "ae", "œ", "oe")

// accents is the Combining Diacritical Marks block.
var accents = &unicode.RangeTable{R16: []unicode.Range16{{Lo: 0x0300, Hi: 0x036F, Stride: 1}}}

// Fold makes text comparable without regard to case or accents.
func Fold(s string) string {
	t := transform.Chain(norm.NFKD, runes.Remove(runes.In(accents)), norm.NFC)
	folded, _, err := transform.String(t, strings.ToLower(s))
	if err != nil {
		folded = strings.ToLower(s)
	}
	return foldLetters.Replace(folded)
}

// SearchWords splits what was typed in the search box into the words a
// song must contain.
func SearchWords(query string) []string {
	return strings.Fields(Fold(query))
}
