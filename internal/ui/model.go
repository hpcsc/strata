package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/hpcsc/strata/internal/keymap"
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

type CheckRefs struct{}

type refsRead struct {
	refs string
	err  error
}

type treeChanged struct {
	tree stack.Tree
	err  error
}

type planLoaded struct {
	plan       restack.Plan
	err        error
	rebaseDone bool
	notice     string
	keepPlan   bool
}

type stacksMoved struct {
	stack   string
	result  restack.Result
	err     error
	plan    restack.Plan
	planErr error
}

type resolveStarted struct {
	pending restack.Pending
	err     error
}

type shellExited struct{}

type deleteChecked struct {
	deletions []stack.Deletion
	err       error
}

type branchesDeleted struct {
	deletions []stack.Deletion
	err       error
}

type Options struct {
	Keys  keymap.Keymap
	Split bool
}

type Model struct {
	ctx         context.Context
	sources     Sources
	keys        keymap.Keymap
	cache       *cache
	stack       stackPanel
	files       filesPanel
	diff        diffPanel
	focus       focus
	zoomed      bool
	help        bool
	helpOffset  int
	width       int
	height      int
	stackHeight int
	filesWidth  int
	status      string
	// filesKey names the branch whose files the files panel shows.
	filesKey string
	// wantPath is the file to select when the next branch's files arrive.
	wantPath   string
	prompt     prompt
	initial    tea.Cmd
	work       *work
	spins      int
	notice     string
	rebaseDone bool
	// refs is the text of the refs that the tree on the screen comes from.
	refs     string
	checking bool
	// wantCheck asks for a check that starts when no check runs and strata is
	// not busy.
	wantCheck bool
}

func New(ctx context.Context, tree stack.Tree, sources Sources, opts Options) Model {
	m := Model{
		ctx:     ctx,
		sources: sources,
		keys:    opts.Keys,
		cache:   newCache(ctx, sources),
		stack:   newStackPanel(tree),
		files:   newFilesPanel(sources.Viewed),
		diff:    diffPanel{theme: sources.Highlighter.Theme(), split: opts.Split},
		refs:    tree.Refs,
	}
	m.initial = m.showSelection()
	return m
}

func (m Model) Init() tea.Cmd {
	return m.initial
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := m.update(msg)
	if !m.wantCheck || m.checking || m.busy() {
		return m, cmd
	}
	m.wantCheck, m.checking = false, true
	return m, tea.Batch(cmd, m.readRefs())
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.ColorProfileMsg:
		m.diff.setColorProfile(msg.Profile)
		return m, nil
	case treeLoaded:
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.status, m.notice = msg.status, msg.notice
		m.cache.clearCommitsAndErrors()
		m.keepFile()
		m.refs = msg.tree.Refs
		m.stack.plan, m.rebaseDone = nil, false
		m.stack.replace(msg.tree)
		m.layout()
		return m, m.showSelection()
	case CheckRefs:
		m.wantCheck = true
		return m, nil
	case refsRead:
		if msg.err != nil || msg.refs == m.refs {
			m.checking = false
			return m, nil
		}
		if m.busy() {
			m.checking, m.wantCheck = false, true
			return m, nil
		}
		return m, m.readChangedTree()
	case treeChanged:
		m.checking = false
		if msg.err != nil {
			return m, nil
		}
		if m.busy() {
			m.wantCheck = true
			return m, nil
		}
		m.keepFile()
		m.refs = msg.tree.Refs
		m.stack.replace(msg.tree)
		m.layout()
		return m, m.showSelection()
	case workTicked:
		if msg.work != m.work {
			return m, nil
		}
		m.spins++
		return m, m.work.tick()
	case stacksMoved:
		m.work, m.rebaseDone = nil, false
		if msg.err != nil {
			m.stack.plan = nil
			return m, m.readTree(msg.err.Error(), "")
		}
		m.status, m.notice = moveReport(msg.stack, msg.result)
		if msg.planErr != nil {
			m.stack.plan = nil
			return m, m.readTree(strings.TrimSpace(m.status+" "+msg.planErr.Error()), m.notice)
		}
		m.stack.showPlanAfterMove(msg.plan)
		m.cache.clear()
		m.filesKey = ""
		m.layout()
		return m, m.showSelection()
	case planLoaded:
		m.work = nil
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
		m.rebaseDone = msg.rebaseDone
		m.focus = focusStack
		m.layout()
		return m, m.showSelection()
	case resolveStarted:
		m.work = nil
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		if msg.pending.State != restack.RebaseWaits {
			return m, m.startWork("reading the sync rebase", m.readSync(false))
		}
		return m, tea.ExecProcess(shellIn(msg.pending), func(error) tea.Msg { return shellExited{} })
	case shellExited:
		return m, m.startWork("reading the sync rebase", m.readSync(false))
	case deleteChecked:
		m.work = nil
		if msg.err != nil {
			m.status = msg.err.Error()
			return m, nil
		}
		m.stack.deletion = msg.deletions
		return m, nil
	case branchesDeleted:
		m.work = nil
		if msg.err != nil {
			return m, m.readTree(msg.err.Error(), "")
		}
		return m, m.readTree("", deletedReport(msg.deletions))
	}
	if m.cache.store(msg) {
		return m, m.showSelection()
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.help {
		return m.helpKey(msg.String()), nil
	}
	m.status, m.notice = "", ""
	if m.prompt.open {
		return m.promptKey(msg)
	}
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.stack.deletion != nil {
		deletions := m.stack.deletion
		m.stack.deletion = nil
		if key != "y" {
			return m, nil
		}
		return m, m.deleteBranches(deletions)
	}

	switch m.action(key) {
	case keymap.Quit:
		return m, tea.Quit
	case keymap.Help:
		m.help, m.helpOffset = true, 0
	case keymap.StartSearch:
		m.prompt.start(m.focus, m.query(m.focus).String())
	case keymap.NextPanel:
		if !m.zoomed {
			m.focus = (m.focus + 1) % focusCount
		}
	case keymap.PreviousPanel:
		if !m.zoomed {
			m.focus = (m.focus + focusCount - 1) % focusCount
		}
	case keymap.NextBranch, keymap.StackDown:
		return m, m.stepBranch(1)
	case keymap.PreviousBranch, keymap.StackUp:
		return m, m.stepBranch(-1)
	case keymap.ToggleSplit:
		m.diff.toggleSplit()
	case keymap.ToggleWholeFile:
		m.diff.toggleWholeFile()
	case keymap.ToggleZoom:
		m.zoomed = !m.zoomed
		if m.zoomed {
			m.focus = focusDiff
		}
		m.layout()
	case keymap.ToggleTree:
		m.files.toggleFlat()
	case keymap.ToggleViewed:
		if m.focus != focusStack {
			return m, m.toggleViewed()
		}
	case keymap.Refresh:
		return m, m.reloadTree()
	case keymap.Sync:
		return m, m.planSync()
	case keymap.StackTop:
		return m, m.branchAtRow(0)
	case keymap.StackBottom:
		return m, m.branchAtRow(len(m.stack.shown) - 1)
	case keymap.StackOpen:
		m.focus = focusFiles
	case keymap.StackMark:
		if m.stack.toggleMark() {
			return m, m.showSelection()
		}
	case keymap.StackDelete:
		return m, m.checkDelete()
	case keymap.FilesDown:
		return m, m.moveRow(m.files.cursor + 1)
	case keymap.FilesUp:
		return m, m.moveRow(m.files.cursor - 1)
	case keymap.FilesTop:
		return m, m.moveRow(0)
	case keymap.FilesBottom:
		return m, m.moveRow(len(m.files.rows) - 1)
	case keymap.FilesFold:
		if m.files.toggleFolder() {
			return m, m.showSelection()
		}
	case keymap.FilesOpen:
		m.focus = focusDiff
	case keymap.FilesBack:
		m.focus = focusStack
	case keymap.FilesHalfPageDown, keymap.DiffHalfPageDown:
		m.diff.scroll(m.diff.height / 2)
	case keymap.FilesHalfPageUp, keymap.DiffHalfPageUp:
		m.diff.scroll(-m.diff.height / 2)
	case keymap.DiffDown:
		m.diff.scroll(1)
	case keymap.DiffUp:
		m.diff.scroll(-1)
	case keymap.DiffPageDown:
		m.diff.scroll(m.diff.height - 1)
	case keymap.DiffPageUp:
		m.diff.scroll(-(m.diff.height - 1))
	case keymap.DiffTop:
		m.diff.toTop()
	case keymap.DiffBottom:
		m.diff.toBottom()
	case keymap.NextHunk:
		m.diff.nextHunk()
	case keymap.PreviousHunk:
		m.diff.previousHunk()
	case keymap.NextFile:
		return m, m.moveToFile(1)
	case keymap.PreviousFile:
		return m, m.moveToFile(-1)
	case keymap.DiffBack:
		if m.zoomed {
			m.zoomed = false
			m.layout()
		} else {
			m.focus = focusFiles
		}
	case keymap.ClearSearch:
		return m, m.setQuery(m.focus, "")
	case keymap.NextMatch:
		m.diff.nextMatch()
	case keymap.PreviousMatch:
		m.diff.previousMatch()
	case keymap.MoveStack:
		return m, m.moveStack()
	case keymap.ResolveConflict:
		return m, m.resolveConflict()
	case keymap.ClosePlan:
		m.stack.closePlan()
		m.rebaseDone = false
		m.cache.clear()
		m.filesKey = ""
		m.layout()
		return m, m.showSelection()
	}
	return m, nil
}

var panelTables = [focusCount]keymap.Table{focusStack: keymap.InStack, focusFiles: keymap.InFiles, focusDiff: keymap.InDiff}

func (m Model) action(key string) keymap.Action {
	if m.help {
		return m.keys.Action(keymap.InDiff, key)
	}
	var tables []keymap.Table
	if m.stack.plan != nil {
		tables = append(tables, keymap.InPlan)
	}
	if !m.query(m.focus).Empty() {
		tables = append(tables, keymap.InSearch)
	}
	for _, t := range append(tables, keymap.Anywhere, panelTables[m.focus]) {
		if a := m.keys.Action(t, key); a != keymap.None && m.applies(a) {
			return a
		}
	}
	return keymap.None
}

func (m Model) applies(a keymap.Action) bool {
	switch a {
	case keymap.MoveStack, keymap.ResolveConflict, keymap.ClosePlan:
		return m.focus == focusStack
	case keymap.NextMatch, keymap.PreviousMatch:
		return m.focus == focusDiff
	case keymap.StackMark, keymap.StackDelete:
		return m.sources.Deleter != nil && m.stack.plan == nil
	}
	return true
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

func (m Model) promptKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
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
		m.diff.setFind(query)
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
	if m.sources.Sync == nil || m.work != nil {
		return nil
	}
	if err := m.sources.Sync.Available(); err != nil {
		m.status = err.Error()
		return nil
	}
	return m.startWork("planning the sync", m.readSync(true))
}

func (m Model) readSync(thenPlan bool) func(report func(step string)) tea.Msg {
	ctx, sync := m.ctx, m.sources.Sync
	return func(report func(step string)) tea.Msg {
		pending, err := sync.Pending(ctx)
		if err != nil {
			return planLoaded{err: err}
		}
		notice := ""
		switch pending.State {
		case restack.RebaseWaits:
			return planLoaded{err: pending.WaitsError()}
		case restack.RebaseDone:
			return planLoaded{plan: pending.Plan, rebaseDone: true}
		case restack.RebaseAborted:
			if _, err := sync.Finish(ctx); err != nil {
				return planLoaded{err: err}
			}
			notice = "You aborted the sync rebase for the stack of " + pending.Stack + ", and no branch moved."
		}
		if !thenPlan {
			return planLoaded{notice: notice, keepPlan: true}
		}
		next, err := sync.Plan(ctx, func(step string) { report("planning the sync: " + step) })
		return planLoaded{plan: next, err: err, notice: notice}
	}
}

func (m *Model) resolveConflict() tea.Cmd {
	b, ok := m.stack.selected()
	if !ok || m.work != nil || m.stack.plan == nil || m.rebaseDone || !m.stack.plan.StackHasConflict(b.Name) {
		return nil
	}
	ctx, sync, plan := m.ctx, m.sources.Sync, *m.stack.plan
	return m.startWork("starting the sync rebase", func(func(string)) tea.Msg {
		pending, err := sync.Resolve(ctx, plan, b.Name)
		return resolveStarted{pending: pending, err: err}
	})
}

func (m *Model) moveStack() tea.Cmd {
	b, ok := m.stack.selected()
	if !ok || m.work != nil {
		return nil
	}
	plan := m.stack.plan.ForStackOf(b.Name)
	root := plan.Tree.Branches[0].Name
	if !plan.StackMoves(root) {
		m.notice = "the stack of " + root + " does not move"
		return nil
	}
	ctx, sync, rebaseDone := m.ctx, m.sources.Sync, m.rebaseDone
	return m.startWork("moving the stack of "+root, func(report func(string)) tea.Msg {
		var result restack.Result
		var err error
		if rebaseDone {
			result, err = sync.Finish(ctx)
		} else {
			result, err = sync.Move(ctx, plan)
		}
		if err != nil {
			return stacksMoved{err: err}
		}
		next, err := sync.Replan(ctx, func(step string) { report("planning the other stacks: " + step) })
		return stacksMoved{stack: root, result: result, plan: next, planErr: err}
	})
}

func (m *Model) checkDelete() tea.Cmd {
	branches := m.stack.deleteTargets()
	if len(branches) == 0 || m.work != nil {
		return nil
	}
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.Name
	}
	if err := m.stack.tree.CheckDelete(names); err != nil {
		m.status = err.Error()
		return nil
	}
	ctx, deleter := m.ctx, m.sources.Deleter
	return m.startWork("reading what the delete loses", func(func(string)) tea.Msg {
		deletions, err := deleter.Check(ctx, branches)
		return deleteChecked{deletions: deletions, err: err}
	})
}

func (m *Model) deleteBranches(deletions []stack.Deletion) tea.Cmd {
	ctx, deleter := m.ctx, m.sources.Deleter
	return m.startWork("deleting "+plural(len(deletions), "branch"), func(func(string)) tea.Msg {
		return branchesDeleted{deletions: deletions, err: deleter.Delete(ctx, deletions)}
	})
}

func deletedReport(deletions []stack.Deletion) string {
	var branches, worktrees []string
	for _, del := range deletions {
		b := del.Branch
		branches = append(branches, b.Name+" (was "+b.Tip[:min(7, len(b.Tip))]+")")
		if del.RemovesWorktree != "" {
			worktrees = append(worktrees, filepath.Base(del.RemovesWorktree))
		}
	}
	report := "Deleted " + joinAnd(branches) + "."
	switch len(worktrees) {
	case 0:
	case 1:
		report += " Removed worktree " + worktrees[0] + "."
	default:
		report += " Removed worktrees " + joinAnd(worktrees) + "."
	}
	return report
}

func joinAnd(parts []string) string {
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func moveReport(stack string, result restack.Result) (status, notice string) {
	if len(result.Stayed) > 0 {
		s := result.Stayed[0]
		return "The stack of " + s.Stack + " stays: " + s.Reason, ""
	}
	if result.Moved == 0 {
		return "", "The stack of " + stack + " did not move."
	}
	return "", "Moved the stack of " + stack + "."
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

// busy reports whether a new tree must wait: it closes the sync plan and
// cancels a delete that waits for y.
func (m Model) busy() bool {
	return m.work != nil || m.stack.plan != nil || m.stack.deletion != nil
}

func (m Model) readRefs() tea.Cmd {
	ctx, tree := m.ctx, m.sources.Tree
	return func() tea.Msg {
		refs, err := tree.Refs(ctx)
		return refsRead{refs: refs, err: err}
	}
}

func (m Model) readChangedTree() tea.Cmd {
	ctx, tree := m.ctx, m.sources.Tree
	return func() tea.Msg {
		t, err := tree.Read(ctx)
		return treeChanged{tree: t, err: err}
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
		if ready {
			m.diff.showNotice(key, "no files changed")
		} else {
			// the diff keeps its key, so it keeps its scroll position when the same file shows again
			m.diff.showNotice(m.diff.key, "loading…")
		}
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
		m.diff.show(fileKey(b, f), result.source)
	}
	return tea.Batch(cmds...)
}

func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	body := m.height - 1
	if m.zoomed {
		m.diff.setSize(m.width-2, body-2)
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
	m.diff.setSize(m.width-m.filesWidth-2, bottom-2)
}

func (m Model) View() tea.View {
	v := tea.NewView(m.screen())
	v.AltScreen = true
	return v
}

func (m Model) screen() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	if m.help {
		lines, rows := m.keyRows()
		if len(lines) <= rows {
			return box(joinHints("Keys", "any key closes"), lines, m.width, m.height, true)
		}
		title := joinHints("Keys", m.hint("scroll", keymap.DiffDown, keymap.DiffUp), "any other key closes")
		return box(title, window(lines, min(m.helpOffset, len(lines)-rows), rows), m.width, m.height, true)
	}
	footer := m.footer()
	if m.zoomed {
		title, lines := m.rightPanel()
		return box(title, lines, m.width, m.height-1, true) + "\n" + footer
	}
	bottom := m.height - 1 - m.stackHeight
	stackTitle := "Stack"
	switch {
	case m.stack.plan != nil:
		stackTitle = "Stack · sync plan"
	case m.stack.deletion != nil:
		stackTitle = "Stack · delete " + plural(len(m.stack.deletion), "branch")
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
	title := "Files · " + b.Name
	if len(m.files.files) > 0 {
		title += " · " + plural(len(m.files.files), "file")
	}
	return title
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
	if m.diff.wholeFile {
		title += " · whole"
	}
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
	switch {
	case b.RebaseWorktree != "":
		lines = append(lines, " "+warningText.Render("a rebase in "+b.RebaseWorktree+" uses it"))
	case b.Worktree != "":
		lines = append(lines, " "+dimText.Render("checked out in "+b.Worktree))
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
	if m.work != nil {
		return " " + selectedText.Render(spinner[m.spins%len(spinner)]) + " " + truncate(m.work.text()+"…", max(0, m.width-4))
	}
	if d := m.stack.deletion; d != nil {
		style := warningText
		if d.lostFiles() > 0 {
			style = errorText
		}
		return style.Render(" " + d.prompt())
	}
	if m.notice != "" {
		return viewedText.Render(" " + m.notice)
	}
	line := m.hints()
	if query := m.query(m.focus); !query.Empty() {
		line = joinHints("/"+query.String(), m.searchResult(m.focus), m.hint("clear", keymap.ClearSearch), line)
	}
	return dimText.Render(" " + truncate(line, max(0, m.width-2)))
}

func (m Model) hints() string {
	switch {
	case m.focus == focusStack && m.stack.plan != nil:
		return m.planHints()
	case m.focus == focusStack:
		sync := ""
		if m.canSync() {
			sync = m.hint("sync", keymap.Sync)
		}
		marked := ""
		if n := len(m.stack.marked); n > 0 {
			marked = fmt.Sprintf("%d marked", n)
		}
		return joinHints(marked, m.hint("branch", keymap.StackDown, keymap.StackUp), m.hint("files", keymap.StackOpen),
			m.hint("mark", keymap.StackMark), m.hint("delete", keymap.StackDelete), m.hint("panel", keymap.NextPanel),
			m.hint("split", keymap.ToggleSplit), m.hint("zoom", keymap.ToggleZoom), m.hint("refresh", keymap.Refresh), sync,
			m.hint("keys", keymap.Help), m.hint("quit", keymap.Quit))
	case m.focus == focusFiles:
		return joinHints(m.hint("move", keymap.FilesDown, keymap.FilesUp), m.hint("fold", keymap.FilesFold),
			m.hint("diff", keymap.FilesOpen), m.hint("viewed", keymap.ToggleViewed),
			m.hint("branch", keymap.PreviousBranch, keymap.NextBranch), m.hint("tree", keymap.ToggleTree),
			m.hint("back", keymap.FilesBack), m.hint("split", keymap.ToggleSplit), m.hint("zoom", keymap.ToggleZoom),
			m.hint("keys", keymap.Help))
	}
	return joinHints(m.hint("scroll", keymap.DiffDown, keymap.DiffUp), m.hint("page", keymap.DiffHalfPageDown, keymap.DiffHalfPageUp),
		m.hint("hunk", keymap.NextHunk, keymap.PreviousHunk), m.hint("file", keymap.NextFile, keymap.PreviousFile), m.hint("viewed", keymap.ToggleViewed),
		m.hint("branch", keymap.PreviousBranch, keymap.NextBranch), m.hint("split", keymap.ToggleSplit),
		m.hint("whole", keymap.ToggleWholeFile), m.hint("zoom", keymap.ToggleZoom), m.hint("keys", keymap.Help))
}

func (m Model) planHints() string {
	move, resolve := "", ""
	if b, ok := m.stack.selected(); ok {
		switch {
		case m.rebaseDone:
			move = m.hint("move the resolved stack", keymap.MoveStack)
		case m.stack.plan.StackMoves(b.Name):
			move = m.hint("move this stack", keymap.MoveStack)
		}
		if !m.rebaseDone && m.stack.plan.StackHasConflict(b.Name) {
			resolve = m.hint("resolve the conflict", keymap.ResolveConflict)
		}
	}
	return joinHints(m.hint("branch", keymap.StackDown, keymap.StackUp), move, resolve, m.hint("close", keymap.ClosePlan),
		m.hint("panel", keymap.NextPanel), m.hint("keys", keymap.Help), m.hint("quit", keymap.Quit))
}

func (m Model) hint(label string, actions ...keymap.Action) string {
	var keys []string
	for _, a := range actions {
		if i := slices.IndexFunc(m.keys.Keys(a), func(k string) bool { return m.action(k) == a }); i >= 0 {
			keys = append(keys, shortKey(m.keys.Keys(a)[i]))
		}
	}
	if len(keys) == 0 {
		return ""
	}
	return strings.Join(keys, "/") + " " + label
}

func shortKey(key string) string {
	if key == "enter" {
		return "⏎"
	}
	return strings.Replace(key, "ctrl+", "^", 1)
}

func joinHints(hints ...string) string {
	return strings.Join(slices.DeleteFunc(hints, func(h string) bool { return h == "" }), " · ")
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
		return joinHints(m.diff.findStatus(), m.hint("match", keymap.NextMatch, keymap.PreviousMatch))
	}
}

func shellIn(p restack.Pending) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	message := "strata: resolve the conflict of the stack of " + p.Stack + " here, then run git rebase --continue.\n" +
		"git rebase --abort ends the sync rebase. Exit the shell to go back to strata."
	cmd := exec.Command("/bin/sh", "-c", `printf '%s\n' "$1"; exec "$2"`, "sh", message, shell)
	cmd.Dir = p.Worktree
	return cmd
}

type keySection struct {
	title   string
	actions []keymap.Action
}

func (m Model) keySections() []keySection {
	anywhere := keymap.Anywhere.Actions()
	sections := []keySection{{"Anywhere", anywhere}, {"Stack", keymap.InStack.Actions()}}
	if m.canSync() {
		sections = append(sections, keySection{"Sync plan", keymap.InPlan.Actions()})
	} else {
		sections[0].actions = slices.DeleteFunc(anywhere, func(a keymap.Action) bool { return a == keymap.Sync })
	}
	return append(sections,
		keySection{"Files", keymap.InFiles.Actions()},
		keySection{"Diff", keymap.InDiff.Actions()},
		keySection{"While a search is on", keymap.InSearch.Actions()})
}

func (m Model) helpKey(key string) Model {
	lines, rows := m.keyRows()
	hidden := len(lines) - rows
	if hidden <= 0 {
		m.help = false
		return m
	}
	switch m.action(key) {
	case keymap.DiffDown:
		m.helpOffset++
	case keymap.DiffUp:
		m.helpOffset--
	case keymap.DiffHalfPageDown:
		m.helpOffset += rows / 2
	case keymap.DiffHalfPageUp:
		m.helpOffset -= rows / 2
	case keymap.DiffPageDown:
		m.helpOffset += rows
	case keymap.DiffPageUp:
		m.helpOffset -= rows
	case keymap.DiffTop:
		m.helpOffset = 0
	case keymap.DiffBottom:
		m.helpOffset = hidden
	default:
		m.help = false
		return m
	}
	m.helpOffset = max(0, min(m.helpOffset, hidden))
	return m
}

func (m Model) keyRows() (lines []string, height int) {
	height = m.height - 2
	return m.keyLines(m.width-2, height), height
}

const columnGap = "   "

func (m Model) keyLines(width, height int) []string {
	sections := m.keySections()
	var left, right []string
	for split := 1; split < len(sections); split++ {
		l, r := m.keyColumn(sections[:split]), m.keyColumn(sections[split:])
		if left == nil || max(len(l), len(r)) < max(len(left), len(right)) {
			left, right = l, r
		}
	}
	leftWidth, rightWidth := widest(left), widest(right)
	single := m.keyColumn(sections)
	if leftWidth+len(columnGap)+rightWidth > width {
		if len(single) <= height {
			return single
		}
		leftWidth = max(0, min(leftWidth, (width-len(columnGap))/2))
	}
	lines := make([]string, max(len(left), len(right)))
	for i := range lines {
		l, r := "", ""
		if i < len(left) {
			l = truncate(left[i], leftWidth)
		}
		if i < len(right) {
			r = right[i]
		}
		lines[i] = l + strings.Repeat(" ", max(0, leftWidth-lipgloss.Width(l))) + columnGap + r
	}
	return lines
}

func (m Model) keyColumn(sections []keySection) []string {
	width := 0
	for _, s := range sections {
		for _, a := range s.actions {
			width = max(width, lipgloss.Width(strings.Join(m.keys.Keys(a), "  ")))
		}
	}
	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "  "+s.title)
		for _, a := range s.actions {
			keys := strings.Join(m.keys.Keys(a), "  ")
			lines = append(lines, "    "+keys+strings.Repeat(" ", width-lipgloss.Width(keys))+"  "+a.About())
		}
	}
	return lines
}

func widest(lines []string) int {
	width := 0
	for _, line := range lines {
		width = max(width, lipgloss.Width(line))
	}
	return width
}
