package ui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hpcsc/strata/internal/stack"
)

type focus int

const (
	focusStack focus = iota
	focusFiles
	focusDiff
	focusCount
)

type treeLoaded struct {
	tree stack.Tree
	err  error
}

type Model struct {
	ctx         context.Context
	sources     Sources
	cache       *cache
	stack       stackPanel
	files       filesPanel
	diff        diffPanel
	focus       focus
	split       bool
	zoomed      bool
	help        bool
	width       int
	height      int
	stackHeight int
	filesWidth  int
	status      string
	// filesKey names the branch whose files the files panel shows.
	filesKey string
	// wantPath is the file to select when the next branch's files arrive.
	wantPath string
	initial  tea.Cmd
}

func New(ctx context.Context, tree stack.Tree, sources Sources) Model {
	m := Model{
		ctx:     ctx,
		sources: sources,
		cache:   newCache(ctx, sources),
		stack:   newStackPanel(tree),
		split:   true,
	}
	m.initial = m.sync()
	return m
}

func (m Model) Init() tea.Cmd {
	return m.initial
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case treeLoaded:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.status = ""
		m.cache.clear()
		m.filesKey = ""
		m.stack.replace(msg.tree)
		m.layout()
		return m, m.sync()
	}
	if m.cache.store(msg) {
		return m, m.sync()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.help {
		m.help = false
		return m, nil
	}
	m.status = ""

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	case "tab":
		if !m.zoomed {
			m.focus = (m.focus + 1) % focusCount
		}
		return m, nil
	case "shift+tab":
		if !m.zoomed {
			m.focus = (m.focus + focusCount - 1) % focusCount
		}
		return m, nil
	case "[":
		return m, m.moveBranch(m.stack.cursor - 1)
	case "]":
		return m, m.moveBranch(m.stack.cursor + 1)
	case "s":
		m.split = !m.split
		m.diff.render(m.split)
		return m, nil
	case "z":
		m.zoomed = !m.zoomed
		if m.zoomed {
			m.focus = focusDiff
		}
		m.layout()
		return m, nil
	case "r":
		return m, m.reloadTree()
	case "t":
		m.files.toggleFlat()
		return m, nil
	case "v":
		if m.focus != focusStack {
			return m, m.toggleViewed()
		}
	}

	switch m.focus {
	case focusStack:
		switch key {
		case "j", "down":
			return m, m.moveBranch(m.stack.cursor + 1)
		case "k", "up":
			return m, m.moveBranch(m.stack.cursor - 1)
		case "g", "home":
			return m, m.moveBranch(0)
		case "G", "end":
			return m, m.moveBranch(len(m.stack.tree.Branches) - 1)
		case "enter", "l", "right":
			m.focus = focusFiles
		}
	case focusFiles:
		switch key {
		case "j", "down":
			return m, m.moveFile(m.files.cursor + 1)
		case "k", "up":
			return m, m.moveFile(m.files.cursor - 1)
		case "g", "home":
			return m, m.moveFile(0)
		case "G", "end":
			return m, m.moveFile(len(m.files.files) - 1)
		case "enter", "l", "right":
			m.focus = focusDiff
		case "h", "left", "esc":
			m.focus = focusStack
		case "ctrl+d":
			m.diff.scroll(m.diff.height / 2)
		case "ctrl+u":
			m.diff.scroll(-m.diff.height / 2)
		}
	case focusDiff:
		switch key {
		case "j", "down":
			m.diff.scroll(1)
		case "k", "up":
			m.diff.scroll(-1)
		case "ctrl+d":
			m.diff.scroll(m.diff.height / 2)
		case "ctrl+u":
			m.diff.scroll(-m.diff.height / 2)
		case " ", "pgdown", "ctrl+f":
			m.diff.scroll(m.diff.height - 1)
		case "b", "pgup", "ctrl+b":
			m.diff.scroll(-(m.diff.height - 1))
		case "g", "home":
			m.diff.toTop()
		case "G", "end":
			m.diff.toBottom()
		case "n":
			m.diff.nextHunk()
		case "N":
			m.diff.previousHunk()
		case "J":
			return m, m.moveFile(m.files.cursor + 1)
		case "K":
			return m, m.moveFile(m.files.cursor - 1)
		case "h", "left", "esc":
			if m.zoomed {
				m.zoomed = false
				m.layout()
			} else {
				m.focus = focusFiles
			}
		}
	}
	return m, nil
}

func (m *Model) moveBranch(i int) tea.Cmd {
	if f, ok := m.files.selected(); ok {
		m.wantPath = f.Path
	}
	if !m.stack.moveTo(i) {
		return nil
	}
	return m.sync()
}

func (m *Model) moveFile(i int) tea.Cmd {
	if !m.files.moveTo(i) {
		return nil
	}
	if f, ok := m.files.selected(); ok {
		m.wantPath = f.Path
	}
	return m.sync()
}

func (m *Model) toggleViewed() tea.Cmd {
	f, ok := m.files.selected()
	if !ok {
		return nil
	}
	if err := m.sources.Viewed.Toggle(f); err != nil {
		m.status = err.Error()
		return nil
	}
	if m.sources.Viewed.Has(f) {
		return m.moveFile(m.files.cursor + 1)
	}
	return nil
}

func (m Model) reloadTree() tea.Cmd {
	ctx, tree := m.ctx, m.sources.Tree
	return func() tea.Msg {
		t, err := tree.Read(ctx)
		return treeLoaded{tree: t, err: err}
	}
}

// sync points the files and diff panels at the selected branch and file, and
// returns the commands that load whatever is not cached yet.
func (m *Model) sync() tea.Cmd {
	b, ok := m.stack.selected()
	if !ok {
		m.files.showNotice("no branches")
		m.diff.showNotice("", "no branches")
		return nil
	}
	cmds := []tea.Cmd{m.cache.requestFiles(b), m.cache.requestCommits(b)}

	key := branchKey(b)
	list, ready := m.cache.fileList(b)
	switch {
	case !ready:
		m.files.showNotice("loading…")
		m.filesKey = ""
	case list.err != nil:
		m.files.showNotice(list.err.Error())
		m.filesKey = key
	case m.filesKey != key:
		m.files.setFiles(list.files, m.wantPath)
		m.filesKey = key
	}

	f, ok := m.files.selected()
	if !ok {
		notice := "no files changed"
		if !ready {
			notice = "loading…"
		}
		m.diff.showNotice(key, notice)
		return tea.Batch(cmds...)
	}
	cmds = append(cmds, m.cache.requestPatch(b, f))
	if next := m.files.cursor + 1; next < len(m.files.files) {
		cmds = append(cmds, m.cache.requestPatch(b, m.files.files[next]))
	}
	result, ready := m.cache.patch(b, f)
	switch {
	case !ready:
		m.diff.showNotice(fileKey(b, f), "loading…")
	case result.err != nil:
		m.diff.showNotice(fileKey(b, f), result.err.Error())
	default:
		m.diff.show(fileKey(b, f), result.source, m.split)
	}
	return tea.Batch(cmds...)
}

func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	body := m.height - 1
	if m.zoomed {
		m.diff.setSize(m.width-2, body-2, m.split)
		return
	}
	m.stackHeight = min(max(len(m.stack.tree.Branches)+3, 4), max(4, body/3))
	m.filesWidth = max(24, min(m.width*28/100, 56))
	if m.width-m.filesWidth < 40 {
		m.filesWidth = max(12, m.width/3)
	}
	bottom := body - m.stackHeight
	m.stack.setHeight(m.stackHeight - 2)
	m.files.setHeight(bottom - 2)
	m.diff.setSize(m.width-m.filesWidth-2, bottom-2, m.split)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.help {
		return box("Keys · any key closes", helpLines, m.width, m.height, true)
	}
	footer := m.footer()
	if m.zoomed {
		return box(m.diffTitle(), m.diff.lines(), m.width, m.height-1, true) + "\n" + footer
	}
	bottom := m.height - 1 - m.stackHeight
	stackBox := box("Stack", m.stack.lines(m.width-2, m.focus == focusStack, m.progress), m.width, m.stackHeight, m.focus == focusStack)
	filesBox := box(m.filesTitle(), m.files.lines(m.filesWidth-2, m.focus == focusFiles, m.sources.Viewed), m.filesWidth, bottom, m.focus == focusFiles)
	right := box(m.diffTitle(), m.diff.lines(), m.width-m.filesWidth, bottom, m.focus == focusDiff)
	if m.focus == focusStack {
		right = box(m.branchTitle(), m.summaryLines(), m.width-m.filesWidth, bottom, false)
	}
	return stackBox + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, filesBox, right) + "\n" + footer
}

func (m Model) progress(b stack.Branch) string {
	list, ok := m.cache.fileList(b)
	if !ok || list.err != nil || len(list.files) == 0 {
		return ""
	}
	viewed := 0
	for _, f := range list.files {
		if m.sources.Viewed.Has(f) {
			viewed++
		}
	}
	if viewed == 0 {
		return ""
	}
	return fmt.Sprintf("✓ %d/%d viewed", viewed, len(list.files))
}

func (m Model) filesTitle() string {
	b, ok := m.stack.selected()
	if !ok {
		return "Files"
	}
	return "Files · " + b.Name
}

func (m Model) branchTitle() string {
	b, _ := m.stack.selected()
	return "Branch · " + b.Name
}

func (m Model) diffTitle() string {
	f, ok := m.files.selected()
	if !ok {
		return "Diff"
	}
	title := f.Path
	if f.OldPath != "" && f.OldPath != f.Path {
		title += " ← " + f.OldPath
	}
	title += fmt.Sprintf(" · %d/%d · %s", m.files.cursor+1, len(m.files.files), m.diff.position())
	if m.sources.Viewed.Has(f) {
		title += " · ✓ viewed"
	}
	return title
}

func (m Model) summaryLines() []string {
	b, ok := m.stack.selected()
	if !ok {
		return nil
	}
	lines := []string{" " + boldText.Render(b.Name) + dimText.Render(" on ") + b.Parent}
	if b.Parent != m.stack.tree.Trunk && b.Behind > 0 {
		lines = append(lines, " "+warningText.Render(fmt.Sprintf("%s has %s that %s lacks: restack it",
			b.Parent, plural(b.Behind, "commit"), b.Name)))
	}
	lines = append(lines, "")
	switch list, ready := m.cache.commitList(b); {
	case !ready:
		lines = append(lines, " "+dimText.Render("loading commits…"))
	case list.err != nil:
		lines = append(lines, " "+errorText.Render(list.err.Error()))
	default:
		lines = append(lines, " "+plural(len(list.commits), "commit"))
		for _, c := range list.commits {
			lines = append(lines, "   "+modifiedText.Render(c.Hash)+" "+c.Subject+" "+dimText.Render(c.Age))
		}
	}
	lines = append(lines, "", " "+plural(b.Files, "file")+"  "+
		addedText.Render(fmt.Sprintf("+%d", b.Insertions))+" "+deletedText.Render(fmt.Sprintf("-%d", b.Deletions)))
	if p := m.progress(b); p != "" {
		lines = append(lines, " "+viewedText.Render(p))
	}
	return lines
}

func (m Model) footer() string {
	if m.status != "" {
		return errorText.Render(" " + m.status)
	}
	hints := map[focus]string{
		focusStack: "j/k branch · ⏎ files · tab panel · s split · z zoom · r refresh · ? keys · q quit",
		focusFiles: "j/k file · ⏎ diff · v viewed · [ ] branch · t tree · h back · s split · z zoom · ? keys · q quit",
		focusDiff:  "j/k scroll · ^d/^u page · n/N hunk · J/K file · v viewed · [ ] branch · s split · z zoom · ? keys",
	}
	return dimText.Render(" " + truncate(hints[m.focus], max(0, m.width-2)))
}

var helpLines = strings.Split(strings.TrimPrefix(`
  Anywhere
    [ ]          previous or next branch; the same file stays selected when it can
    tab          next panel (shift+tab: previous panel)
    s            side by side or unified diff
    z            diff on the full screen
    v            mark the file viewed, then go to the next file
    t            files as a tree or as a list of paths
    r            read the branches again
    q            quit

  Stack
    j/k  g/G     move between branches
    enter        go to the files of the branch

  Files
    j/k  g/G     move between files
    enter        go to the diff
    ctrl+d/u     scroll the diff
    h  esc       back to the stack

  Diff
    j/k          scroll one line
    ctrl+d/u     scroll half a page (space and b: a full page)
    g/G          top or bottom
    n/N          next or previous hunk
    J/K          next or previous file
    h  esc       back to the files (in zoom, esc ends the zoom first)
`, "\n"), "\n")
