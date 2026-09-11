package syntax

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

type Color struct {
	R, G, B uint8
	Set     bool
}

type Span struct {
	Text   string
	Color  Color
	Bold   bool
	Italic bool
}

type Highlighter struct {
	style *chroma.Style
}

func NewHighlighter(styleName string) *Highlighter {
	return &Highlighter{style: styles.Get(styleName)}
}

// Lines highlights content in the language its path names and returns one
// span list per line: entry i holds line i+1.
func (h *Highlighter) Lines(path, content string) [][]Span {
	if content == "" {
		return nil
	}
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, content)
	if err != nil {
		return plain(content)
	}
	var out [][]Span
	for _, tokens := range chroma.SplitTokensIntoLines(iterator.Tokens()) {
		spans := make([]Span, 0, len(tokens))
		for _, tok := range tokens {
			text := strings.TrimSuffix(strings.TrimSuffix(tok.Value, "\n"), "\r")
			if text == "" {
				continue
			}
			entry := h.style.Get(tok.Type)
			spans = append(spans, Span{
				Text:   text,
				Color:  color(entry.Colour),
				Bold:   entry.Bold == chroma.Yes,
				Italic: entry.Italic == chroma.Yes,
			})
		}
		out = append(out, spans)
	}
	return out
}

func plain(content string) [][]Span {
	var out [][]Span
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		out = append(out, []Span{{Text: strings.TrimSuffix(line, "\r")}})
	}
	return out
}

func color(c chroma.Colour) Color {
	if !c.IsSet() {
		return Color{}
	}
	return Color{R: c.Red(), G: c.Green(), B: c.Blue(), Set: true}
}
