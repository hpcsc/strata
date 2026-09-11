package diff

import (
	"regexp"
	"strings"
)

// Range is a byte range in a line; End is exclusive.
type Range struct {
	Start int
	End   int
}

// maxTokenPairs caps the comparison table, which grows with the product of
// the two lines' token counts.
const maxTokenPairs = 20000

// minSharedPercent leaves a rewritten line unmarked: when two lines share
// fewer tokens than this, marking every token only adds noise.
const minSharedPercent = 30

var tokenPattern = regexp.MustCompile(`[\p{L}\p{N}_]+|\s+|.`)

type token struct {
	text  string
	start int
	end   int
	space bool
}

func ChangedWords(deleted, added string) (deletedRanges, addedRanges []Range) {
	before, after := tokenize(deleted), tokenize(added)
	if len(before) == 0 || len(after) == 0 || len(before)*len(after) > maxTokenPairs {
		return nil, nil
	}
	keptBefore, keptAfter := commonTokens(before, after)
	shared, total := 0, max(wordCount(before), wordCount(after))
	for i, t := range before {
		if keptBefore[i] && !t.space {
			shared++
		}
	}
	if total == 0 || shared*100/total < minSharedPercent {
		return nil, nil
	}
	return changedRanges(before, keptBefore), changedRanges(after, keptAfter)
}

func tokenize(line string) []token {
	var tokens []token
	for _, loc := range tokenPattern.FindAllStringIndex(line, -1) {
		text := line[loc[0]:loc[1]]
		tokens = append(tokens, token{
			text:  text,
			start: loc[0],
			end:   loc[1],
			space: strings.TrimSpace(text) == "",
		})
	}
	return tokens
}

func wordCount(tokens []token) int {
	n := 0
	for _, t := range tokens {
		if !t.space {
			n++
		}
	}
	return n
}

func commonTokens(a, b []token) (keptA, keptB []bool) {
	lengths := make([][]int, len(a)+1)
	for i := range lengths {
		lengths[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i].text == b[j].text {
				lengths[i][j] = lengths[i+1][j+1] + 1
			} else {
				lengths[i][j] = max(lengths[i+1][j], lengths[i][j+1])
			}
		}
	}
	keptA, keptB = make([]bool, len(a)), make([]bool, len(b))
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i].text == b[j].text:
			keptA[i], keptB[j] = true, true
			i++
			j++
		case lengths[i+1][j] >= lengths[i][j+1]:
			i++
		default:
			j++
		}
	}
	return keptA, keptB
}

func changedRanges(tokens []token, kept []bool) []Range {
	var ranges []Range
	for i, t := range tokens {
		if kept[i] {
			continue
		}
		if n := len(ranges); n > 0 && ranges[n-1].End == t.start {
			ranges[n-1].End = t.end
			continue
		}
		ranges = append(ranges, Range{Start: t.start, End: t.end})
	}
	return ranges
}
