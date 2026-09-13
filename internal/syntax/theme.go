package syntax

import "github.com/alecthomas/chroma/v2"

type Theme struct {
	Text       Color
	Background Color
	Inserted   Color
	Deleted    Color
	Subheading Color
	LineNumber Color
	Comment    Color
}

func (h *Highlighter) Theme() Theme {
	base := h.style.Get(chroma.Background)
	return Theme{
		Text:       color(base.Colour),
		Background: color(base.Background),
		Inserted:   h.marking(chroma.GenericInserted),
		Deleted:    h.marking(chroma.GenericDeleted),
		Subheading: color(h.style.Get(chroma.GenericSubheading).Colour),
		LineNumber: color(h.style.Get(chroma.LineNumbers).Colour),
		Comment:    color(h.style.Get(chroma.Comment).Colour),
	}
}

// styles mark t in either the text colour or the background, and Get fills
// whichever one t leaves unset from the plain text style.
func (h *Highlighter) marking(t chroma.TokenType) Color {
	if !h.style.Has(t) {
		return Color{}
	}
	base, entry := h.style.Get(chroma.Background), h.style.Get(t)
	var best Color
	if entry.Colour != base.Colour {
		best = color(entry.Colour)
	}
	if entry.Background != base.Background && color(entry.Background).colorfulness() > best.colorfulness() {
		best = color(entry.Background)
	}
	return best
}

func (c Color) colorfulness() int {
	if !c.Set {
		return -1
	}
	return int(max(c.R, c.G, c.B)) - int(min(c.R, c.G, c.B))
}
