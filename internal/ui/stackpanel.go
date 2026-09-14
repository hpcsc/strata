package ui

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/hpcsc/strata/internal/search"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/tmux"
)

type stackPanel struct {
	tree           stack.Tree
	plan           *restack.Plan
	treeBeforePlan stack.Tree
	filter         search.Query
	marked         map[string]bool
	deletion       deletion
	// shown holds the indexes in tree.Branches that show: the branches that
	// match the filter or are marked, and the branches they sit on.
	shown  []int
	cursor int
	offset int
	height int
}

type deletion []stack.Deletion

func (d deletion) of(name string) (stack.Deletion, bool) {
	i := slices.IndexFunc(d, func(del stack.Deletion) bool { return del.Branch.Name == name })
	if i < 0 {
		return stack.Deletion{}, false
	}
	return d[i], true
}

func (d deletion) lostFiles() int {
	n := 0
	for _, del := range d {
		n += len(del.LostFiles)
	}
	return n
}

func (d deletion) closedPanes() int {
	n := 0
	for _, del := range d {
		n += len(del.ClosesPanes)
	}
	return n
}

func (d deletion) prompt() string {
	worktrees := 0
	for _, del := range d {
		if del.RemovesWorktree != "" {
			worktrees++
		}
	}
	parts := []string{"delete " + plural(len(d), "branch")}
	if worktrees > 0 {
		parts = append(parts, "remove "+plural(worktrees, "worktree"))
	}
	if n := d.closedPanes(); n > 0 {
		parts = append(parts, "close "+plural(n, "tmux pane"))
	}
	if n := d.lostFiles(); n > 0 {
		parts = append(parts, "lose "+plural(n, "changed file"))
	}
	return "y " + joinAnd(parts) + " · any other key cancels"
}

func lossText(del stack.Deletion) string {
	text := warningText.Render("loses " + plural(del.Branch.Commits, "commit"))
	if del.TrunkHas {
		text = viewedText.Render("the trunk has its changes")
	}
	folder := filepath.Base(del.RemovesWorktree)
	switch {
	case del.RemovesWorktree == "":
		return text
	case del.WorktreeGone:
		text += dimText.Render(" · forgets worktree " + folder + ", whose folder is gone")
	case len(del.LostFiles) > 0:
		text += errorText.Render(" · removes worktree " + folder + " and loses " +
			plural(len(del.LostFiles), "changed file") + ": " + strings.Join(del.LostFiles, ", "))
	default:
		text += warningText.Render(" · removes worktree " + folder)
	}
	if len(del.ClosesPanes) > 0 {
		text += warningText.Render(" · closes " + plural(len(del.ClosesPanes), "tmux pane") + " in " + joinAnd(tmuxWindows(del.ClosesPanes)))
	}
	return text
}

func tmuxWindows(panes []tmux.Pane) []string {
	var names []string
	for _, p := range panes {
		if !slices.Contains(names, p.Window) {
			names = append(names, p.Window)
		}
	}
	return names
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
	if len(p.marked) > 0 {
		kept := map[string]bool{}
		for name := range p.marked {
			if tree.Index(name) >= 0 {
				kept[name] = true
			}
		}
		p.marked = kept
	}
	p.deletion = nil
	p.showBranches()
	p.keepCursorShown()
}

func (p *stackPanel) toggleMark() (selectionChanged bool) {
	b, ok := p.selected()
	if !ok {
		return false
	}
	before := p.cursor
	marked := maps.Clone(p.marked)
	if marked == nil {
		marked = map[string]bool{}
	}
	if marked[b.Name] {
		delete(marked, b.Name)
	} else {
		marked[b.Name] = true
	}
	p.marked = marked
	p.showBranches()
	p.keepCursorShown()
	return p.cursor != before
}

func (p stackPanel) deleteTargets() []stack.Branch {
	var branches []stack.Branch
	for _, b := range p.tree.Branches {
		if p.marked[b.Name] {
			branches = append(branches, b)
		}
	}
	if b, ok := p.selected(); ok && len(branches) == 0 {
		branches = append(branches, b)
	}
	return branches
}

func (p *stackPanel) showPlan(plan restack.Plan) {
	if p.plan == nil {
		p.treeBeforePlan = p.tree
	}
	p.plan = &plan
	p.replace(plan.Tree)
}

func (p *stackPanel) showPlanAfterMove(plan restack.Plan) {
	p.treeBeforePlan = plan.Tree
	if p.plan == nil {
		p.replace(plan.Tree)
		return
	}
	plan.NewCommits = p.plan.NewCommits
	p.plan = &plan
	p.replace(plan.Tree)
}

func (p *stackPanel) closePlan() {
	if p.plan == nil {
		return
	}
	p.plan = nil
	p.replace(p.treeBeforePlan)
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
		if !p.filter.Matches(b.Name) && !p.marked[b.Name] {
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

	trunk := "  " + boldText.Render(p.tree.Trunk)
	if p.plan != nil {
		trunk += "  " + dimText.Render(plural(p.plan.NewCommits, "new commit"))
	}
	rows := []string{trunk}
	for row, b := range shownTree.Branches {
		bar, mark := " ", " "
		if p.marked[b.Name] {
			mark = errorText.Render("●")
		}
		nameStyle := lipgloss.NewStyle()
		if b.Name == p.tree.Current {
			nameStyle = currentText
		}
		if p.shown[row] == p.cursor {
			bar = dimText.Render("▌")
			if focused {
				bar = selectedText.Render("▌")
				nameStyle = selectedText
			}
		}
		gutter := bar + mark
		if !p.filter.Matches(b.Name) {
			nameStyle = dimText
		}
		prefix := prefixes[row]
		name := truncate(b.Name, max(1, nameWidth-lipgloss.Width(prefix)))
		label := dimText.Render(prefix) + highlight(name, p.filter, nameStyle) +
			strings.Repeat(" ", max(0, nameWidth-lipgloss.Width(prefix)-lipgloss.Width(name)))

		if p.plan != nil {
			o := p.plan.Outcomes[b.Name]
			rows = append(rows, gutter+label+"  "+outcomeStyle(o).Render(o.Text()))
			continue
		}
		if del, ok := p.deletion.of(b.Name); ok {
			rows = append(rows, gutter+label+"  "+lossText(del))
			continue
		}
		stats := dimText.Render(fmt.Sprintf("%-11s %-9s", plural(b.Commits, "commit"), plural(b.Files, "file"))) +
			" " + addedText.Render(fmt.Sprintf("+%d", b.Insertions)) + " " + deletedText.Render(fmt.Sprintf("-%d", b.Deletions))
		if b.Parent != p.tree.Trunk && b.Behind > 0 {
			stats += "  " + warningText.Render(fmt.Sprintf("%d behind parent", b.Behind))
		}
		if s := progress(b); s != "" {
			stats += "  " + viewedText.Render(s)
		}
		switch {
		case b.RebaseWorktree != "":
			stats += "  " + warningText.Render("rebase in "+filepath.Base(b.RebaseWorktree))
		case b.Worktree != "" && b.Name != p.tree.Current:
			stats += "  " + dimText.Render("in "+filepath.Base(b.Worktree))
		}
		rows = append(rows, gutter+label+"  "+stats)
	}
	return window(rows, p.offset, p.height)
}

func outcomeStyle(o restack.Outcome) lipgloss.Style {
	if o.Blocker != "" {
		return dimText
	}
	switch o.Kind {
	case restack.UpToDate:
		return dimText
	case restack.Moves:
		return addedText
	case restack.Merged:
		return viewedText
	default:
		return warningText
	}
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
