package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hpcsc/strata/internal/stack"
)

type stackPanel struct {
	tree   stack.Tree
	cursor int
	offset int
	height int
}

func newStackPanel(tree stack.Tree) stackPanel {
	p := stackPanel{tree: tree}
	if i := tree.Index(tree.Current); i >= 0 {
		p.cursor = i
	}
	return p
}

func (p stackPanel) selected() (stack.Branch, bool) {
	if p.cursor < 0 || p.cursor >= len(p.tree.Branches) {
		return stack.Branch{}, false
	}
	return p.tree.Branches[p.cursor], true
}

func (p *stackPanel) moveTo(i int) bool {
	i = max(0, min(i, len(p.tree.Branches)-1))
	if i == p.cursor {
		return false
	}
	p.cursor = i
	p.follow()
	return true
}

func (p *stackPanel) replace(tree stack.Tree) {
	name := ""
	if b, ok := p.selected(); ok {
		name = b.Name
	}
	p.tree = tree
	if i := tree.Index(name); i >= 0 {
		p.cursor = i
	} else {
		p.cursor = max(0, min(p.cursor, len(tree.Branches)-1))
	}
	p.follow()
}

func (p *stackPanel) setHeight(height int) {
	p.height = height
	p.follow()
}

// follow scrolls so the cursor's row is visible; row 0 is the trunk.
func (p *stackPanel) follow() {
	row := p.cursor + 1
	if p.cursor == 0 {
		p.offset = 0
	}
	if row < p.offset {
		p.offset = row
	}
	if p.height > 0 && row >= p.offset+p.height {
		p.offset = row - p.height + 1
	}
}

func (p stackPanel) lines(width int, focused bool, progress func(stack.Branch) string) []string {
	prefixes := p.tree.Connectors()
	nameWidth := 0
	for i, b := range p.tree.Branches {
		nameWidth = max(nameWidth, lipgloss.Width(prefixes[i]+b.Name))
	}
	nameWidth = min(nameWidth, max(20, width*6/10))

	rows := []string{"  " + boldText.Render(p.tree.Trunk)}
	for i, b := range p.tree.Branches {
		gutter := "  "
		nameStyle := lipgloss.NewStyle()
		if b.Name == p.tree.Current {
			nameStyle = currentText
		}
		if i == p.cursor {
			gutter = dimText.Render("▌") + " "
			if focused {
				gutter = selectedText.Render("▌") + " "
				nameStyle = selectedText
			}
		}
		prefix := prefixes[i]
		name := truncate(b.Name, max(1, nameWidth-lipgloss.Width(prefix)))
		label := dimText.Render(prefix) + nameStyle.Render(name) +
			strings.Repeat(" ", max(0, nameWidth-lipgloss.Width(prefix)-lipgloss.Width(name)))

		stats := dimText.Render(fmt.Sprintf("%-11s %-9s", plural(b.Commits, "commit"), plural(b.Files, "file"))) +
			" " + addedText.Render(fmt.Sprintf("+%d", b.Insertions)) + " " + deletedText.Render(fmt.Sprintf("-%d", b.Deletions))
		if b.Parent != p.tree.Trunk && b.Behind > 0 {
			stats += "  " + warningText.Render(fmt.Sprintf("%d behind parent", b.Behind))
		}
		if s := progress(b); s != "" {
			stats += "  " + viewedText.Render(s)
		}
		rows = append(rows, gutter+label+"  "+stats)
	}
	return window(rows, p.offset, p.height)
}

func window(rows []string, offset, height int) []string {
	if height <= 0 || offset >= len(rows) {
		return nil
	}
	return rows[max(0, offset):min(len(rows), offset+height)]
}
