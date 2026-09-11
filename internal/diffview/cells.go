package diffview

import (
	"strings"
	"unicode"

	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/mattn/go-runewidth"
)

const tabWidth = 4

type cell struct {
	text   string
	width  int
	color  syntax.Color
	bold   bool
	italic bool
	marked bool
	found  bool
}

func layOut(text string, spans []syntax.Span, marks, found []diff.Range) []cell {
	if joined(spans) != text {
		spans = []syntax.Span{{Text: text}}
	}
	var out []cell
	offset, column := 0, 0
	for _, span := range spans {
		for i, r := range span.Text {
			c := cell{
				color: span.Color, bold: span.Bold, italic: span.Italic,
				marked: inside(offset+i, marks), found: inside(offset+i, found),
			}
			switch {
			case r == '\t':
				for n := tabWidth - column%tabWidth; n > 0; n-- {
					c.text, c.width = " ", 1
					out = append(out, c)
					column++
				}
				continue
			case unicode.IsControl(r):
				c.text, c.width = "·", 1
			default:
				c.text, c.width = string(r), runewidth.RuneWidth(r)
			}
			if c.width == 0 {
				if len(out) > 0 {
					out[len(out)-1].text += c.text
				}
				continue
			}
			out = append(out, c)
			column += c.width
		}
		offset += len(span.Text)
	}
	return out
}

func wrap(cells []cell, width int) [][]cell {
	rows := [][]cell{nil}
	used := 0
	for _, c := range cells {
		if used+c.width > width && used > 0 {
			rows = append(rows, nil)
			used = 0
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], c)
		used += c.width
	}
	return rows
}

func joined(spans []syntax.Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

func inside(offset int, ranges []diff.Range) bool {
	for _, r := range ranges {
		if offset >= r.Start && offset < r.End {
			return true
		}
	}
	return false
}
