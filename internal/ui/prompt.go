package ui

import (
	tea "github.com/charmbracelet/bubbletea"
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
func (p *prompt) edit(msg tea.KeyMsg) (changed, cancelled bool) {
	switch msg.Type {
	case tea.KeyEnter:
		p.open = false
	case tea.KeyEsc, tea.KeyCtrlC:
		p.open, p.text = false, ""
		return true, true
	case tea.KeyBackspace:
		if p.text == "" {
			return false, false
		}
		runes := []rune(p.text)
		p.text = string(runes[:len(runes)-1])
		return true, false
	case tea.KeyCtrlU:
		p.text = ""
		return true, false
	case tea.KeySpace:
		p.text += " "
		return true, false
	case tea.KeyRunes:
		if msg.Alt {
			return false, false
		}
		p.text += string(msg.Runes)
		return true, false
	}
	return false, false
}

func (p prompt) view(result string) string {
	line := selectedText.Render(" /") + p.text + selectedText.Render("▏")
	if result != "" {
		line += "  " + dimText.Render(result)
	}
	return line + dimText.Render("  · ⏎ keep · esc clear")
}
