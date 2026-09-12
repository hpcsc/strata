package ui

import (
	tea "charm.land/bubbletea/v2"
)

// prompt reads a search query on the bottom line of the screen, for the
// panel that had the focus when it opened.
type prompt struct {
	open   bool
	target focus
	text   string
}

func (p *prompt) start(target focus, text string) {
	p.open, p.target, p.text = true, target, text
}

// edit applies a key to the query. It reports whether the text changed, and
// whether the reader cancelled the search with esc.
func (p *prompt) edit(msg tea.KeyPressMsg) (changed, cancelled bool) {
	switch msg.String() {
	case "enter":
		p.open = false
		return false, false
	case "esc", "ctrl+c":
		p.open, p.text = false, ""
		return true, true
	case "backspace":
		if p.text == "" {
			return false, false
		}
		runes := []rune(p.text)
		p.text = string(runes[:len(runes)-1])
		return true, false
	case "ctrl+u":
		p.text = ""
		return true, false
	}
	if msg.Text == "" || msg.Mod.Contains(tea.ModAlt) || msg.Mod.Contains(tea.ModCtrl) {
		return false, false
	}
	p.text += msg.Text
	return true, false
}

func (p prompt) view(result string) string {
	line := selectedText.Render(" /") + p.text + selectedText.Render("▏")
	if result != "" {
		line += "  " + dimText.Render(result)
	}
	return line + dimText.Render("  · ⏎ keep · esc clear")
}
