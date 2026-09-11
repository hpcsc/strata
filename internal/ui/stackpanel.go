package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/hpcsc/strata/internal/search"
	"github.com/hpcsc/strata/internal/stack"
)

type stackPanel struct {
	tree   stack.Tree
	filter search.Query
	// shown holds the indexes in tree.Branches that show: the branches that
	// match the filter and the branches they sit on.
	shown  []int
	cursor int
	offset int
	height int
}

func newStackPanel(tree stack.Tree) stackPanel {
	p := stackPanel{tree: tree}
	if i := tree.Index(tree.Current); i >= 0 {
		p.cursor = i
	}
	p.showBranches()
	return p
}

func (p stackPanel) selected() (stack.Branch, bool) {
	if p.cursor < 0 || p.cursor >= len(p.tree.Branches) {
		return stack.Branch{}, false
	}
	return p.tree.Branches[p.cursor], true
}

func (p *stackPanel) replace(tree stack.Tree) {
	name := ""
	if b, ok := p.selected(); ok {
		name = b.Name
	}
	p.tree = tree
	p.cursor = max(0, min(p.cursor, len(tree.Branches)-1))
	if i := tree.Index(name); i >= 0 {
		p.cursor = i
	}
	p.showBranches()
	p.keepCursorShown()
}

// setFilter shows the branches that match filter. It reports whether the
// selected branch changed, which happens when the filter hides it.
func (p *stackPanel) setFilter(filter search.Query) bool {
	before := p.cursor
	p.filter = filter
	p.showBranches()
	if b, ok := p.selected(); ok && !filter.Matches(b.Name) {
		for _, i := range p.shown {
			if filter.Matches(p.tree.Branches[i].Name) {
				p.cursor = i
				break
			}
		}
	}
	p.keepCursorShown()
	return p.cursor != before
}

func (p *stackPanel) showBranches() {
	keep := make([]bool, len(p.tree.Branches))
	for i, b := range p.tree.Branches {
		if !p.filter.Matches(b.Name) {
			continue
		}
		for at := i; at >= 0 && !keep[at]; at = p.tree.Index(p.tree.Branches[at].Parent) {
			keep[at] = true
		}
	}
	p.shown = nil
	for i, kept := range keep {
		if kept {
			p.shown = append(p.shown, i)
		}
	}
}

func (p *stackPanel) keepCursorShown() {
	if p.row(p.cursor) < 0 && len(p.shown) > 0 {
		p.cursor = p.shown[0]
	}
	p.follow()
}

// row returns the place of branch i among the shown branches, or -1.
func (p stackPanel) row(i int) int {
	for row, shown := range p.shown {
		if shown == i {
			return row
		}
	}
	return -1
}

func (p stackPanel) matching() int {
	n := 0
	for _, i := range p.shown {
		if p.filter.Matches(p.tree.Branches[i].Name) {
			n++
		}
	}
	return n
}

func (p *stackPanel) step(by int) bool {
	row := p.row(p.cursor) + by
	if row < 0 || row >= len(p.shown) {
		return false
	}
	p.cursor = p.shown[row]
	p.follow()
	return true
}

func (p *stackPanel) moveToRow(row int) bool {
	if len(p.shown) == 0 {
		return false
	}
	row = max(0, min(row, len(p.shown)-1))
	if p.shown[row] == p.cursor {
		return false
	}
	p.cursor = p.shown[row]
	p.follow()
	return true
}

func (p *stackPanel) setHeight(height int) {
	p.height = height
	p.follow()
}

// follow scrolls so the cursor's row is visible; row 0 is the trunk.
func (p *stackPanel) follow() {
	row := p.row(p.cursor) + 1
	if row <= 1 {
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
	shownTree := stack.Tree{Trunk: p.tree.Trunk}
	for _, i := range p.shown {
		shownTree.Branches = append(shownTree.Branches, p.tree.Branches[i])
	}
	prefixes := shownTree.Connectors()
	nameWidth := 0
	for row, b := range shownTree.Branches {
		nameWidth = max(nameWidth, lipgloss.Width(prefixes[row]+b.Name))
	}
	nameWidth = min(nameWidth, max(20, width*6/10))

	rows := []string{"  " + boldText.Render(p.tree.Trunk)}
	for row, b := range shownTree.Branches {
		gutter := "  "
		nameStyle := lipgloss.NewStyle()
		if b.Name == p.tree.Current {
			nameStyle = currentText
		}
		if p.shown[row] == p.cursor {
			gutter = dimText.Render("▌") + " "
			if focused {
				gutter = selectedText.Render("▌") + " "
				nameStyle = selectedText
			}
		}
		if !p.filter.Matches(b.Name) {
			nameStyle = dimText
		}
		prefix := prefixes[row]
		name := truncate(b.Name, max(1, nameWidth-lipgloss.Width(prefix)))
		label := dimText.Render(prefix) + highlight(name, p.filter, nameStyle) +
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

// highlight underlines the parts of text that match query.
func highlight(text string, query search.Query, base lipgloss.Style) string {
	var b strings.Builder
	at := 0
	for _, r := range query.Ranges(text) {
		b.WriteString(base.Render(text[at:r.Start]))
		b.WriteString(base.Underline(true).Bold(true).Render(text[r.Start:r.End]))
		at = r.End
	}
	b.WriteString(base.Render(text[at:]))
	return b.String()
}

func window(rows []string, offset, height int) []string {
	if height <= 0 || offset >= len(rows) {
		return nil
	}
	return rows[max(0, offset):min(len(rows), offset+height)]
}
