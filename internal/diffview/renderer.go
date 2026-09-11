package diffview

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/search"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/mattn/go-runewidth"
)

type renderer struct {
	src         Source
	width       int
	numberWidth int
	find        search.Query
}

func newRenderer(src Source, width int, find search.Query) renderer {
	widest := 1
	for _, h := range src.Patch.Hunks {
		for _, l := range h.Lines {
			widest = max(widest, len(strconv.Itoa(max(l.OldNumber, l.NewNumber))))
		}
	}
	return renderer{src: src, width: width, numberWidth: widest, find: find}
}

// unified returns the rows of a hunk, and the index of each row that starts
// a line with a match.
func (r renderer) unified(h diff.Hunk) ([]string, []int) {
	marks := changedWords(h.Lines)
	contentWidth := r.width - ansi.StringWidth(r.unifiedGutter(diff.Line{}, true))
	var out []string
	var matches []int
	for i, l := range h.Lines {
		found := r.found(l)
		if len(found) > 0 {
			matches = append(matches, len(out))
		}
		for k, row := range wrap(layOut(l.Text, r.spans(l), marks[i], found), contentWidth) {
			out = append(out, r.unifiedGutter(l, k == 0)+paint(row, contentWidth, l.Kind))
		}
	}
	return out, matches
}

func (r renderer) unifiedGutter(l diff.Line, first bool) string {
	old, new := number(l.OldNumber), number(l.NewNumber)
	sign, signColor := marker(l.Kind)
	if !first {
		old, new, sign = "", "", " "
	}
	return paintText(fmt.Sprintf("%*s %*s ", r.numberWidth, old, r.numberWidth, new), style{fg: numberColor}) +
		paintText("│", style{fg: separatorColor}) +
		paintText(sign+" ", style{fg: signColor, bg: lineBackground(l.Kind)})
}

// split returns the rows of a hunk side by side, and the index of each row
// that starts a pair of lines with a match on either side.
func (r renderer) split(h diff.Hunk) ([]string, []int) {
	marks := changedWords(h.Lines)
	leftWidth := (r.width - 1) / 2
	rightWidth := r.width - 1 - leftWidth
	separator := paintText("│", style{fg: separatorColor})
	var out []string
	var matches []int
	for _, pair := range sideBySide(h.Lines) {
		if r.pairFound(h.Lines, pair) {
			matches = append(matches, len(out))
		}
		left, leftFill := r.sideRows(h.Lines, pair.left, marks, leftWidth, true)
		right, rightFill := r.sideRows(h.Lines, pair.right, marks, rightWidth, false)
		for k := 0; k < max(len(left), len(right)); k++ {
			out = append(out, rowOrFill(left, k, leftWidth, leftFill)+separator+rowOrFill(right, k, rightWidth, rightFill))
		}
	}
	return out, matches
}

func (r renderer) pairFound(lines []diff.Line, pair sidePair) bool {
	return (pair.left >= 0 && len(r.found(lines[pair.left])) > 0) ||
		(pair.right >= 0 && len(r.found(lines[pair.right])) > 0)
}

func (r renderer) found(l diff.Line) []diff.Range {
	var out []diff.Range
	for _, m := range r.find.Ranges(l.Text) {
		out = append(out, diff.Range{Start: m.Start, End: m.End})
	}
	return out
}

// sideRows returns one side of a split row, and the style that pads it when
// the other side wraps onto more rows.
func (r renderer) sideRows(lines []diff.Line, index int, marks [][]diff.Range, width int, before bool) ([]string, style) {
	if index < 0 {
		return nil, style{bg: emptySideFiller}
	}
	l := lines[index]
	n := l.NewNumber
	if before {
		n = l.OldNumber
	}
	contentWidth := width - r.numberWidth - 1
	var out []string
	for k, row := range wrap(layOut(l.Text, r.spans(l), marks[index], r.found(l)), contentWidth) {
		label := ""
		if k == 0 {
			label = number(n)
		}
		gutter := paintText(fmt.Sprintf("%*s ", r.numberWidth, label), style{fg: numberColor, bg: lineBackground(l.Kind)})
		out = append(out, gutter+paint(row, contentWidth, l.Kind))
	}
	return out, style{bg: lineBackground(l.Kind)}
}

func (r renderer) hunkHeader(h diff.Hunk) string {
	text := fmt.Sprintf("@@ -%d,%d +%d,%d @@", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	if h.Section != "" {
		text += " " + h.Section
	}
	return fill(runewidth.Truncate(text, r.width, "…"), r.width, style{fg: hunkColor, bg: hunkBackground})
}

func (r renderer) note(text string) string {
	return fill(runewidth.Truncate("  "+text, r.width, "…"), r.width, style{fg: noteColor, italic: true})
}

func (r renderer) spans(l diff.Line) []syntax.Span {
	lines, n := r.src.After, l.NewNumber
	if l.Kind == diff.Deletion {
		lines, n = r.src.Before, l.OldNumber
	}
	if n < 1 || n > len(lines) {
		return nil
	}
	return lines[n-1]
}

type sidePair struct {
	left  int
	right int
}

// sideBySide puts context lines on both sides and pairs each run of deleted
// lines with the added lines that follow it; -1 leaves a side empty.
func sideBySide(lines []diff.Line) []sidePair {
	var pairs []sidePair
	for i := 0; i < len(lines); {
		if lines[i].Kind == diff.Context {
			pairs = append(pairs, sidePair{left: i, right: i})
			i++
			continue
		}
		var deleted, added []int
		for ; i < len(lines) && lines[i].Kind != diff.Context; i++ {
			if lines[i].Kind == diff.Deletion {
				deleted = append(deleted, i)
			} else {
				added = append(added, i)
			}
		}
		for k := 0; k < max(len(deleted), len(added)); k++ {
			p := sidePair{left: -1, right: -1}
			if k < len(deleted) {
				p.left = deleted[k]
			}
			if k < len(added) {
				p.right = added[k]
			}
			pairs = append(pairs, p)
		}
	}
	return pairs
}

func changedWords(lines []diff.Line) [][]diff.Range {
	marks := make([][]diff.Range, len(lines))
	for _, p := range sideBySide(lines) {
		if p.left < 0 || p.right < 0 || p.left == p.right {
			continue
		}
		marks[p.left], marks[p.right] = diff.ChangedWords(lines[p.left].Text, lines[p.right].Text)
	}
	return marks
}

func paint(row []cell, width int, kind diff.LineKind) string {
	var b strings.Builder
	background, word := lineBackground(kind), wordBackground(kind)
	used := 0
	last := style{}
	for i, c := range row {
		s := style{fg: textColor, bg: background, bold: c.bold, italic: c.italic}
		if c.color.Set {
			s.fg = fromSyntax(c.color)
		}
		if c.marked {
			s.bg = word
		}
		if c.found {
			s.fg, s.bg = foundText, foundBackground
		}
		if i == 0 || s != last {
			b.WriteString(sgr(s))
			last = s
		}
		b.WriteString(c.text)
		used += c.width
	}
	if used < width {
		b.WriteString(sgr(style{bg: background}))
		b.WriteString(strings.Repeat(" ", width-used))
	}
	b.WriteString(reset)
	return b.String()
}

func fill(text string, width int, s style) string {
	return paintText(text+strings.Repeat(" ", max(0, width-runewidth.StringWidth(text))), s)
}

func rowOrFill(rows []string, k, width int, filler style) string {
	if k < len(rows) {
		return rows[k]
	}
	return paintText(strings.Repeat(" ", width), filler)
}

func number(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}
