package search

import (
	"strings"
	"unicode/utf8"
)

// Range is a byte range in the searched text; End is exclusive.
type Range struct {
	Start int
	End   int
}

// Query matches the way vim's smartcase does: a query with a capital letter
// matches case exactly, and any other query ignores case.
type Query struct {
	text          string
	caseSensitive bool
}

func New(text string) Query {
	return Query{text: text, caseSensitive: strings.ToLower(text) != text}
}

func (q Query) String() string {
	return q.text
}

func (q Query) Empty() bool {
	return q.text == ""
}

// Matches reports whether s holds the query. An empty query matches anything.
func (q Query) Matches(s string) bool {
	return q.Empty() || len(q.Ranges(s)) > 0
}

// Ranges returns every place in s that matches, left to right and without
// overlaps. It compares rune by rune, so the ranges stay correct for text
// whose lower case has a different length in bytes.
func (q Query) Ranges(s string) []Range {
	if q.Empty() {
		return nil
	}
	runes := utf8.RuneCountInString(q.text)
	var out []Range
	for start := 0; start < len(s); {
		end, counted := start, 0
		for counted < runes && end < len(s) {
			_, size := utf8.DecodeRuneInString(s[end:])
			end += size
			counted++
		}
		if counted == runes && q.equal(s[start:end]) {
			out = append(out, Range{Start: start, End: end})
			start = end
			continue
		}
		_, size := utf8.DecodeRuneInString(s[start:])
		start += size
	}
	return out
}

func (q Query) equal(s string) bool {
	if q.caseSensitive {
		return s == q.text
	}
	return strings.EqualFold(s, q.text)
}
