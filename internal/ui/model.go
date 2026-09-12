package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/hpcsc/strata/internal/search"
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
	tree   stack.Tree
	err    error
	status string
	notice string
}

type planLoaded struct {
	plan     restack.Plan
	err      error
	resolved bool
	notice   string
	keepPlan bool
}

type stacksMoved struct {
	result restack.Result
	err    error
}

type resolveStarted struct {
	pending restack.Pending
	err     error
}

type shellExited struct{}

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
	prompt   prompt
	initial  tea.Cmd
	busy     string
	notice   string
	// resolved is true when the plan on the screen is the stack of a sync
	// rebase that is done, which enter moves with Finish.
	resolved bool
}

func New(ctx context.Context, tree stack.Tree, sources Sources) Model {
	m := Model{
		ctx:     ctx,
		sources: sources,
		cache:   newCache(ctx, sources),
		stack:   newStackPanel(tree),
		files:   newFilesPanel(sources.Viewed),
		split:   true,
	}
	m.initial = m.showSelection()
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
		m.status, m.notice = msg.status, msg.notice
		m.cache.clear()
		m.filesKey = ""
		m.stack.plan, m.resolved = nil, false
		m.stack.replace(msg.tree)
		m.layout()
		return m, m.showSelection()
	case stacksMoved:
		m.busy = ""
		m.stack.plan, m.resolved = nil, false
		if msg.err != nil {
			return m, m.readTree(msg.err.Error(), "")
		}
		report := "Moved " + plural(msg.result.Moved, "stack") + "."
		if len(msg.result.Stayed) == 0 {
			return m, m.readTree("", report)
		}
		for _, s := range msg.result.Stayed {
			report += " The stack of " + s.Stack + " stays: " + s.Reason
		}
		return m, m.readTree(report, "")
	case planLoaded:
		m.busy = ""
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.notice = msg.notice
		if msg.keepPlan {
			return m, nil
		}
		m.cache.clear()
		m.filesKey = ""
		m.stack.showPlan(msg.plan)
		m.resolved = msg.resolved
		m.focus = focusStack
		m.layout()
		return m, m.showSelection()
	case resolveStarted:
		m.busy = ""
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		if msg.pending.State != restack.RebaseWaits {
			m.busy = "reading the sync rebase…"
			return m, m.readSync(false)
		}
		return m, tea.ExecProcess(shellIn(msg.pending), func(error) tea.Msg { return shellExited{} })
	case shellExited:
		m.busy = "reading the sync rebase…"
		return m, m.readSync(false)
	}
	if m.cache.store(msg) {
		return m, m.showSelection()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.help {
		m.help = false
		return m, nil
	}
	m.status, m.notice = "", ""
	if m.prompt.open {
		return m.promptKey(msg)
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.help = true
		return m, nil
	case "S":
		return m, m.planSync()
	case "esc":
		if m.stack.plan != nil {
			m.stack.closePlan()
			m.resolved = false
			m.cache.clear()
			m.filesKey = ""
			m.layout()
			return m, m.showSelection()
		}
	case "/":
		m.prompt.start(m.focus, m.query(m.focus).String())
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
		return m, m.stepBranch(-1)
	case "]":
		return m, m.stepBranch(1)
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
			return m, m.stepBranch(1)
		case "k", "up":
			return m, m.stepBranch(-1)
		case "g", "home":
			return m, m.branchAtRow(0)
		case "G", "end":
			return m, m.branchAtRow(len(m.stack.shown) - 1)
		case "esc":
			return m, m.setQuery(focusStack, "")
		case "c":
			return m, m.resolveConflict()
		case "enter", "l", "right":
			if key == "enter" && m.stack.plan != nil {
				return m, m.moveStacks()
			}
			m.focus = focusFiles
		}
	case focusFiles:
		switch key {
		case "j", "down":
			return m, m.moveRow(m.files.cursor + 1)
		case "k", "up":
			return m, m.moveRow(m.files.cursor - 1)
		case "g", "home":
			return m, m.moveRow(0)
		case "G", "end":
			return m, m.moveRow(len(m.files.rows) - 1)
		case "o":
			if m.files.toggleFolder() {
				return m, m.showSelection()
			}
		case "enter", "l", "right":
			m.focus = focusDiff
		case "esc":
			if !m.files.filter.Empty() {
				return m, m.setQuery(focusFiles, "")
			}
			m.focus = focusStack
		case "h", "left":
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
			if m.diff.find.Empty() {
				m.diff.nextHunk()
			} else {
				m.diff.nextMatch()
			}
		case "N":
			if m.diff.find.Empty() {
				m.diff.previousHunk()
			} else {
				m.diff.previousMatch()
			}
		case "J":
			return m, m.moveToFile(1)
		case "K":
			return m, m.moveToFile(-1)
		case "esc":
			if !m.diff.find.Empty() {
				return m, m.setQuery(focusDiff, "")
			}
			if m.zoomed {
				m.zoomed = false
				m.layout()
			} else {
				m.focus = focusFiles
			}
		case "h", "left":
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

func (m *Model) stepBranch(by int) tea.Cmd {
	m.keepFile()
	if !m.stack.step(by) {
		return nil
	}
	return m.showSelection()
}

func (m *Model) branchAtRow(row int) tea.Cmd {
	m.keepFile()
	if !m.stack.moveToRow(row) {
		return nil
	}
	return m.showSelection()
}

// keepFile remembers the selected file, to select it again on the next
// branch when that branch changes it.
func (m *Model) keepFile() {
	if f, ok := m.files.selected(); ok {
		m.wantPath = f.Path
	}
}

func (m Model) promptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	changed, cancelled := m.prompt.edit(msg)
	switch {
	case cancelled:
		return m, m.setQuery(m.prompt.target, "")
	case changed:
		return m, m.setQuery(m.prompt.target, m.prompt.text)
	}
	return m, nil
}

func (m Model) query(target focus) search.Query {
	switch target {
	case focusStack:
		return m.stack.filter
	case focusFiles:
		return m.files.filter
	default:
		return m.diff.find
	}
}

// setQuery filters the stack or the files, or finds text in the diff,
// depending on target.
func (m *Model) setQuery(target focus, text string) tea.Cmd {
	query := search.New(text)
	switch target {
	case focusStack:
		m.keepFile()
		if m.stack.setFilter(query) {
			return m.showSelection()
		}
	case focusFiles:
		if m.files.setFilter(query) {
			return m.fileMoved()
		}
	default:
		m.diff.setFind(query, m.split)
	}
	return nil
}

func (m *Model) moveRow(i int) tea.Cmd {
	if !m.files.moveTo(i) {
		return nil
	}
	return m.fileMoved()
}

func (m *Model) moveToFile(step int) tea.Cmd {
	if !m.files.moveToFile(step) {
		return nil
	}
	return m.fileMoved()
}

func (m *Model) fileMoved() tea.Cmd {
	if f, ok := m.files.selected(); ok {
		m.wantPath = f.Path
	}
	return m.showSelection()
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
		m.files.passViewed(m.files.rows[m.files.cursor].file)
	} else {
		m.files.refresh()
	}
	return m.fileMoved()
}

func (m Model) canSync() bool {
	return m.sources.Sync != nil && m.sources.Sync.Available() == nil
}

func (m *Model) planSync() tea.Cmd {
	if m.sources.Sync == nil || m.busy != "" {
		return nil
	}
	if err := m.sources.Sync.Available(); err != nil {
		m.status = err.Error()
		return nil
	}
	m.busy = "fetching the trunk and planning the sync…"
	return m.readSync(true)
}

func (m Model) readSync(replan bool) tea.Cmd {
	ctx, sync := m.ctx, m.sources.Sync
	return func() tea.Msg {
		pending, err := sync.Pending(ctx)
		if err != nil {
			return planLoaded{err: err}
		}
		notice := ""
		switch pending.State {
		case restack.RebaseWaits:
			return planLoaded{err: pending.WaitsError()}
		case restack.RebaseDone:
			return planLoaded{plan: pending.Plan, resolved: true}
		case restack.RebaseStopped:
			if _, err := sync.Finish(ctx); err != nil {
				return planLoaded{err: err}
			}
			notice = "The sync rebase for the stack of " + pending.Stack + " stopped, and no branch moved."
		}
		if !replan {
			return planLoaded{notice: notice, keepPlan: true}
		}
		plan, err := sync.Plan(ctx)
		return planLoaded{plan: plan, err: err, notice: notice}
	}
}

func (m *Model) resolveConflict() tea.Cmd {
	b, ok := m.stack.selected()
	if !ok || m.busy != "" || m.stack.plan == nil || m.resolved || !m.stack.plan.StackHasConflict(b.Name) {
		return nil
	}
	m.busy = "starting the sync rebase…"
	ctx, sync, plan := m.ctx, m.sources.Sync, *m.stack.plan
	return func() tea.Msg {
		pending, err := sync.Resolve(ctx, plan, b.Name)
		return resolveStarted{pending: pending, err: err}
	}
}

func (m *Model) moveStacks() tea.Cmd {
	if m.busy != "" {
		return nil
	}
	plan := *m.stack.plan
	if plan.StacksThatMove() == 0 {
		m.notice = "the plan moves no stack"
		return nil
	}
	m.busy = "moving the stacks…"
	ctx, sync, resolved := m.ctx, m.sources.Sync, m.resolved
	return func() tea.Msg {
		if resolved {
			result, err := sync.Finish(ctx)
			return stacksMoved{result: result, err: err}
		}
		result, err := sync.Move(ctx, plan)
		return stacksMoved{result: result, err: err}
	}
}

func (m Model) reloadTree() tea.Cmd {
	return m.readTree("", "")
}

func (m Model) readTree(status, notice string) tea.Cmd {
	ctx, tree := m.ctx, m.sources.Tree
	return func() tea.Msg {
		t, err := tree.Read(ctx)
		return treeLoaded{tree: t, err: err, status: status, notice: notice}
	}
}

// showSelection points the files and diff panels at the selected branch and file, and
// returns the commands that load whatever is not cached yet.
func (m *Model) showSelection() tea.Cmd {
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

	if next, ok := m.files.nextFile(); ok {
		cmds = append(cmds, m.cache.requestPatch(b, next))
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
		return box("Keys · any key closes", helpLines(m.canSync()), m.width, m.height, true)
	}
	footer := m.footer()
	if m.zoomed {
		title, lines := m.rightPanel()
		return box(title, lines, m.width, m.height-1, true) + "\n" + footer
	}
	bottom := m.height - 1 - m.stackHeight
	stackTitle := "Stack"
	if m.stack.plan != nil {
		stackTitle = "Stack · sync plan"
	}
	stackBox := box(stackTitle, m.stack.lines(m.width-2, m.focus == focusStack, m.progress), m.width, m.stackHeight, m.focus == focusStack)
	filesBox := box(m.filesTitle(), m.files.lines(m.filesWidth-2, m.focus == focusFiles), m.filesWidth, bottom, m.focus == focusFiles)
	title, lines := m.rightPanel()
	right := box(title, lines, m.width-m.filesWidth, bottom, m.focus == focusDiff)
	return stackBox + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, filesBox, right) + "\n" + footer
}

// rightPanel shows the branch while the stack has the focus, then the
// selected folder or file.
func (m Model) rightPanel() (string, []string) {
	if m.focus == focusStack {
		return m.branchTitle(), m.summaryLines()
	}
	if r, ok := m.files.selectedFolder(); ok {
		return "Folder · " + r.folder, m.folderLines(r)
	}
	return m.diffTitle(), m.diff.lines()
}

func (m Model) folderLines(r fileRow) []string {
	files := m.files.filesIn(r.folder)
	insertions, deletions, viewed, widest := 0, 0, 0, 0
	for _, f := range files {
		insertions, deletions = insertions+f.Insertions, deletions+f.Deletions
		if m.sources.Viewed.Has(f) {
			viewed++
		}
		widest = max(widest, lipgloss.Width(strings.TrimPrefix(f.Path, r.folder)))
	}
	summary := " " + plural(len(files), "file") + "  " + addedText.Render(fmt.Sprintf("+%d", insertions)) + " " +
		deletedText.Render(fmt.Sprintf("-%d", deletions))
	if viewed > 0 {
		summary += "  " + viewedText.Render(fmt.Sprintf("✓ %d/%d viewed", viewed, len(files)))
	}
	lines := []string{" " + boldText.Render(r.folder), summary, ""}
	for _, f := range files {
		name := strings.TrimPrefix(f.Path, r.folder)
		line := "   " + statusStyle(f.Status).Render(string(f.Status)) + "  " + name +
			strings.Repeat(" ", widest-lipgloss.Width(name)) + "  " +
			addedText.Render(fmt.Sprintf("+%d", f.Insertions)) + " " + deletedText.Render(fmt.Sprintf("-%d", f.Deletions))
		if m.sources.Viewed.Has(f) {
			line += "  " + viewedText.Render("✓")
		}
		lines = append(lines, line)
	}
	return lines
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
	at, total := m.files.position()
	title += fmt.Sprintf(" · %d/%d · %s", at, total, m.diff.position())
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
	if m.prompt.open {
		return m.prompt.view(m.searchResult(m.prompt.target))
	}
	if m.status != "" {
		return errorText.Render(" " + m.status)
	}
	if m.busy != "" {
		return dimText.Render(" " + m.busy)
	}
	if m.notice != "" {
		return viewedText.Render(" " + m.notice)
	}
	hints := map[focus]string{
		focusStack: "j/k branch · ⏎ files · tab panel · s split · z zoom · r refresh · ? keys · q quit",
		focusFiles: "j/k move · o fold · ⏎ diff · v viewed · [ ] branch · t tree · h back · s split · z zoom · ? keys",
		focusDiff:  "j/k scroll · ^d/^u page · n/N hunk · J/K file · v viewed · [ ] branch · s split · z zoom · ? keys",
	}
	if m.canSync() {
		hints[focusStack] = strings.Replace(hints[focusStack], "r refresh", "r refresh · S sync", 1)
	}
	if m.stack.plan != nil {
		keys := []string{"j/k branch"}
		switch n := m.stack.plan.StacksThatMove(); {
		case m.resolved:
			keys = append(keys, "enter move the resolved stack")
		case n > 0:
			keys = append(keys, "enter move "+plural(n, "stack"))
		}
		if b, ok := m.stack.selected(); ok && !m.resolved && m.stack.plan.StackHasConflict(b.Name) {
			keys = append(keys, "c resolve the conflict")
		}
		hints[focusStack] = strings.Join(append(keys, "esc close", "tab panel", "? keys", "q quit"), " · ")
	}
	line := hints[m.focus]
	if query := m.query(m.focus); !query.Empty() {
		line = "/" + query.String() + " · " + m.searchResult(m.focus) + " · esc clear · " +
			strings.Replace(line, "n/N hunk · ", "", 1)
	}
	return dimText.Render(" " + truncate(line, max(0, m.width-2)))
}

func (m Model) searchResult(target focus) string {
	if m.query(target).Empty() {
		return ""
	}
	switch target {
	case focusStack:
		return fmt.Sprintf("%d of %d branches", m.stack.matching(), len(m.stack.tree.Branches))
	case focusFiles:
		return fmt.Sprintf("%d of %d files", m.files.matching(), len(m.files.files))
	default:
		return m.diff.findStatus() + " · n/N match"
	}
}

func shellIn(p restack.Pending) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	message := "strata: resolve the conflict of the stack of " + p.Stack + " here, then run git rebase --continue.\n" +
		"git rebase --abort stops the sync rebase. Exit the shell to go back to strata."
	cmd := exec.Command("/bin/sh", "-c", `printf '%s\n' "$1"; exec "$2"`, "sh", message, shell)
	cmd.Dir = p.Worktree
	return cmd
}

func helpLines(canSync bool) []string {
	text := keysText
	if canSync {
		text = strings.Replace(text, "read the branches again\n",
			"read the branches again\n    S            fetch the trunk and show the plan of a sync\n", 1)
		text = strings.Replace(text, "go to the files of the branch\n", "go to the files of the branch\n"+syncKeysText, 1)
	}
	return strings.Split(strings.TrimPrefix(text, "\n"), "\n")
}

const syncKeysText = `
  Sync plan
    enter        move the stacks that the plan moves
    c            resolve the conflict of the stack in a shell
    esc          close the plan
`

const keysText = `
  Anywhere
    [ ]          previous or next branch; the same file stays selected when it can
    tab          next panel (shift+tab: previous panel)
    s            side by side or unified diff
    z            diff on the full screen
    v            mark the file viewed, then go to the next file
    t            files as a tree or as a list of paths
    /            search: filter the stack or the files, or find text in the diff
    esc          clear the search of the panel
    r            read the branches again
    q            quit

  Stack
    j/k  g/G     move between branches
    enter        go to the files of the branch

  Files
    j/k  g/G     move between files and folders
    o            fold or unfold the folder, or the folder that holds the file
    enter        go to the diff
    ctrl+d/u     scroll the diff
    h  esc       back to the stack

  Diff
    j/k          scroll one line
    ctrl+d/u     scroll half a page (space and b: a full page)
    g/G          top or bottom
    n/N          next or previous hunk, or match while a search is on
    J/K          next or previous file, past folded folders
    h  esc       back to the files (in zoom, esc ends the zoom first)
`
