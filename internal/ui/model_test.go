//go:build unit

package ui_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/keymap"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/syntax"
	"github.com/hpcsc/strata/internal/tmux"
	"github.com/hpcsc/strata/internal/ui"
	"github.com/stretchr/testify/require"
)

type memoryTree struct {
	tree    stack.Tree
	commits map[string][]stack.Commit
}

func (m memoryTree) Read(context.Context) (stack.Tree, error) {
	return m.tree, nil
}

func (m memoryTree) Commits(_ context.Context, b stack.Branch) ([]stack.Commit, error) {
	return m.commits[b.Name], nil
}

type memoryDiffs struct {
	files   map[string][]diff.File
	patches map[string]diff.Patch
}

func (m memoryDiffs) Files(_ context.Context, _, branch string) ([]diff.File, error) {
	return m.files[branch], nil
}

func (m memoryDiffs) Patch(_ context.Context, _, branch string, f diff.File) (diff.Patch, error) {
	if patch, ok := m.patches[f.Path]; ok {
		patch.File = f
		return patch, nil
	}
	return diff.Patch{File: f, Hunks: []diff.Hunk{{
		NewStart: 1, NewLines: 1,
		Lines: []diff.Line{{Kind: diff.Addition, Text: "content of " + f.Path + " on " + branch, NewNumber: 1}},
	}}}, nil
}

func (m memoryDiffs) Contents(context.Context, diff.File) (string, string, error) {
	return "", "", nil
}

type plainText struct {
	theme syntax.Theme
}

func (plainText) Lines(string, string) [][]syntax.Span {
	return nil
}

func (p plainText) Theme() syntax.Theme {
	return p.theme
}

type memorySync struct {
	unavailable error
	plan        restack.Plan
	planErr     error
	replanned   restack.Plan
	replanErr   error
	result      restack.Result
	moveErr     error
	moved       []restack.Plan
	pending     restack.Pending
	resolved    []string
	finished    int
}

func (m *memorySync) Available() error {
	return m.unavailable
}

func (m *memorySync) Pending(context.Context) (restack.Pending, error) {
	return m.pending, nil
}

func (m *memorySync) Plan(context.Context, restack.Progress) (restack.Plan, error) {
	return m.plan, m.planErr
}

func (m *memorySync) Replan(context.Context, restack.Progress) (restack.Plan, error) {
	return m.replanned, m.replanErr
}

type steppingSync struct {
	*memorySync
	step     string
	reported chan struct{}
	release  chan struct{}
}

func (s steppingSync) Plan(ctx context.Context, progress restack.Progress) (restack.Plan, error) {
	progress(s.step)
	close(s.reported)
	<-s.release
	return s.memorySync.Plan(ctx, progress)
}

func (m *memorySync) Move(_ context.Context, p restack.Plan) (restack.Result, error) {
	m.moved = append(m.moved, p)
	return m.result, m.moveErr
}

func (m *memorySync) Resolve(_ context.Context, _ restack.Plan, branch string) (restack.Pending, error) {
	m.resolved = append(m.resolved, branch)
	return restack.Pending{State: restack.RebaseWaits, Stack: branch, Worktree: "/work/sync"}, nil
}

func (m *memorySync) Finish(context.Context) (restack.Result, error) {
	m.finished++
	m.pending = restack.Pending{}
	return m.result, nil
}

type memoryDeleter struct {
	deletions map[string]stack.Deletion
	checkErr  error
	err       error
	deleted   [][]string
}

func (m *memoryDeleter) Check(_ context.Context, branches []stack.Branch) ([]stack.Deletion, error) {
	if m.checkErr != nil {
		return nil, m.checkErr
	}
	var deletions []stack.Deletion
	for _, b := range branches {
		del := m.deletions[b.Name]
		del.Branch = b
		deletions = append(deletions, del)
	}
	return deletions, nil
}

func (m *memoryDeleter) Delete(_ context.Context, deletions []stack.Deletion) error {
	var names []string
	for _, del := range deletions {
		names = append(names, del.Branch.Name)
	}
	m.deleted = append(m.deleted, names)
	return m.err
}

type memoryViewed map[string]bool

func (m memoryViewed) Has(f diff.File) bool {
	return m[f.Path]
}

func (m memoryViewed) Toggle(f diff.File) error {
	m[f.Path] = !m[f.Path]
	return nil
}

// keyPress gives the message for a named key, such as "enter", or for text
// that the reader types.
func keyPress(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	if letter, ok := strings.CutPrefix(key, "ctrl+"); ok {
		code, _ := utf8.DecodeRuneInString(letter)
		return tea.KeyPressMsg{Code: code, Mod: tea.ModCtrl}
	}
	code, _ := utf8.DecodeRuneInString(key)
	return tea.KeyPressMsg{Code: code, Text: key}
}

func TestModel(t *testing.T) {
	defaults := func() ui.Options {
		return ui.Options{Keys: keymap.Default(), Split: true}
	}
	file := func(path string) diff.File {
		return diff.File{Path: path, Status: diff.Modified, OldBlob: "old-" + path, NewBlob: "new-" + path, Insertions: 3, Deletions: 1}
	}
	stackOf := func() (stack.Tree, map[string][]diff.File) {
		tree := stack.Tree{Trunk: "origin/main", Current: "events", Branches: []stack.Branch{
			{Name: "events", Parent: "origin/main", Base: "b0", Commits: 2, Files: 2},
			{Name: "handler", Parent: "events", Base: "b1", Level: 1, Commits: 1, Files: 2, Behind: 1},
			{Name: "billing", Parent: "origin/main", Base: "b0", Commits: 1, Files: 1},
		}}
		files := map[string][]diff.File{
			"events":  {file("events.go"), file("order.go")},
			"handler": {file("handler.go"), file("order.go")},
			"billing": {file("invoice.go")},
		}
		return tree, files
	}
	settle := func(m tea.Model, cmd tea.Cmd) tea.Model {
		queue := []tea.Cmd{cmd}
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			if next == nil {
				continue
			}
			switch msg := next().(type) {
			case nil, tea.QuitMsg:
			case tea.BatchMsg:
				queue = append(queue, msg...)
			default:
				var more tea.Cmd
				m, more = m.Update(msg)
				queue = append(queue, more)
			}
		}
		return m
	}
	type modelSources struct {
		tree    stack.Tree
		files   map[string][]diff.File
		patches map[string]diff.Patch
		viewed  memoryViewed
	}
	nestedFiles := func(t *testing.T) modelSources {
		t.Helper()
		tree := stack.Tree{Trunk: "origin/main", Branches: []stack.Branch{{Name: "nested", Parent: "origin/main", Base: "b0"}}}
		return modelSources{tree: tree, viewed: memoryViewed{}, files: map[string][]diff.File{"nested": {
			file("README.md"), file("common/modules/a/one.go"), file("common/modules/a/two.go"), file("common/modules/b/three.go"),
		}}}
	}
	threeHunks := func(t *testing.T) modelSources {
		t.Helper()
		src := nestedFiles(t)
		var hunks []diff.Hunk
		for _, start := range []int{1, 100, 200} {
			var lines []diff.Line
			for i := start; i < start+30; i++ {
				lines = append(lines, diff.Line{Kind: diff.Context, Text: fmt.Sprintf("line %d", i), OldNumber: i, NewNumber: i})
			}
			hunks = append(hunks, diff.Hunk{OldStart: start, OldLines: 30, NewStart: start, NewLines: 30, Lines: lines})
		}
		src.patches = map[string]diff.Patch{"common/modules/a/one.go": {Hunks: hunks}}
		return src
	}
	twoBranches := func(t *testing.T) modelSources {
		t.Helper()
		tree := stack.Tree{Trunk: "origin/main", Current: "first", Branches: []stack.Branch{
			{Name: "first", Parent: "origin/main", Base: "b0"},
			{Name: "second", Parent: "first", Base: "b1", Level: 1},
		}}
		return modelSources{tree: tree, viewed: memoryViewed{}, files: map[string][]diff.File{
			"first":  {file("common/modules/a/one.go"), file("common/modules/a/two.go"), file("common/modules/b/three.go")},
			"second": {file("common/modules/a/one.go"), file("common/modules/c/four.go")},
		}}
	}
	requireInOrder := func(t *testing.T, view string, texts ...string) {
		t.Helper()
		at := 0
		for _, text := range texts {
			i := strings.Index(view[at:], text)
			require.NotEqual(t, -1, i, "%q does not follow the text before it in:\n%s", text, view)
			at += i + len(text)
		}
	}
	requireRow := func(t *testing.T, view string, texts ...string) {
		t.Helper()
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(line, texts[0]) {
				requireInOrder(t, line, texts...)
				return
			}
		}
		require.Fail(t, "no row holds "+texts[0], view)
	}
	columnOf := func(t *testing.T, view, text string) int {
		t.Helper()
		for _, line := range strings.Split(view, "\n") {
			if i := strings.Index(line, text); i >= 0 {
				return ansi.StringWidth(line[:i])
			}
		}
		require.Fail(t, "no row holds "+text, view)
		return -1
	}
	startWithOptions := func(src modelSources, opts ui.Options) tea.Model {
		m := ui.New(context.Background(), src.tree, ui.Sources{
			Tree:        memoryTree{tree: src.tree},
			Diffs:       memoryDiffs{files: src.files, patches: src.patches},
			Highlighter: plainText{},
			Viewed:      src.viewed,
		}, opts)
		var sized tea.Model = m
		sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
		return settle(sized, m.Init())
	}
	startWith := func(src modelSources) tea.Model {
		return startWithOptions(src, defaults())
	}
	start := func(viewed memoryViewed) tea.Model {
		tree, files := stackOf()
		m := ui.New(context.Background(), tree, ui.Sources{
			Tree:        memoryTree{tree: tree, commits: map[string][]stack.Commit{"events": {{Hash: "abc1234", Subject: "Name the events", Age: "2 hours ago"}}}},
			Diffs:       memoryDiffs{files: files},
			Highlighter: plainText{},
			Viewed:      viewed,
		}, defaults())
		var sized tea.Model = m
		sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
		return settle(sized, m.Init())
	}
	press := func(m tea.Model, keys ...string) tea.Model {
		for _, key := range keys {
			var cmd tea.Cmd
			m, cmd = m.Update(keyPress(key))
			m = settle(m, cmd)
		}
		return m
	}
	screen := func(m tea.Model) string {
		return ansi.Strip(m.View().Content)
	}

	t.Run("stack", func(t *testing.T) {
		t.Run("draws each branch under the branch it sits on", func(t *testing.T) {
			view := screen(start(memoryViewed{}))

			require.Contains(t, view, "├─ events")
			require.Contains(t, view, "│  └─ handler")
			require.Contains(t, view, "└─ billing")
			require.Contains(t, view, "1 behind parent")
		})

		t.Run("starts on the checked-out branch and shows its summary beside the files", func(t *testing.T) {
			view := screen(start(memoryViewed{}))

			require.Contains(t, view, "Branch · events")
			require.Contains(t, view, "abc1234 Name the events")
			require.Contains(t, view, "Files · events")
		})

		inWorktrees := func() modelSources {
			tree, files := stackOf()
			tree.Branches[0].Worktree = "/work/strata"
			tree.Branches[1].RebaseWorktree = "/work/strata-handler"
			tree.Branches[2].Worktree = "/work/strata-billing"
			return modelSources{tree: tree, files: files, viewed: memoryViewed{}}
		}

		t.Run("a branch checked out in another worktree shows the folder of that worktree", func(t *testing.T) {
			view := screen(startWith(inWorktrees()))

			require.Regexp(t, `└─ billing\s+1 commit .* in strata-billing`, view)
		})

		t.Run("the branch that strata runs on does not show its worktree", func(t *testing.T) {
			view := screen(startWith(inWorktrees()))

			require.NotRegexp(t, "├─ events[^\n]* in strata", view)
		})

		t.Run("a branch that a rebase uses shows the folder of the rebase", func(t *testing.T) {
			view := screen(startWith(inWorktrees()))

			require.Regexp(t, `└─ handler\s+1 commit .* rebase in strata-handler`, view)
		})

		t.Run("the branch panel shows the full path of the worktree", func(t *testing.T) {
			view := screen(press(startWith(inWorktrees()), "j", "j"))

			require.Contains(t, view, "checked out in /work/strata-billing")
		})
	})

	t.Run("switch branch", func(t *testing.T) {
		t.Run("keeps the same file selected when the next branch changes it too", func(t *testing.T) {
			m := press(start(memoryViewed{}), "enter", "j", "]")

			view := screen(m)
			require.Contains(t, view, "Files · handler")
			require.Contains(t, view, "order.go · 2/2")
			require.Contains(t, view, "content of order.go on handler")
		})

		t.Run("selects the first file when the next branch does not change the selected one", func(t *testing.T) {
			m := press(start(memoryViewed{}), "enter", "]", "]")

			view := screen(m)
			require.Contains(t, view, "Files · billing")
			require.Contains(t, view, "invoice.go · 1/1")
		})
	})

	t.Run("viewed", func(t *testing.T) {
		t.Run("marks the file viewed and moves to the next file", func(t *testing.T) {
			viewed := memoryViewed{}

			m := press(start(viewed), "enter", "v")

			require.True(t, viewed["events.go"])
			view := screen(m)
			require.Contains(t, view, "order.go · 2/2")
			require.Contains(t, view, "✓ 1/2 viewed")
		})
	})

	t.Run("files", func(t *testing.T) {
		t.Run("a path too long for the panel keeps its file name and loses the start of its directory", func(t *testing.T) {
			tree := stack.Tree{Trunk: "origin/main", Branches: []stack.Branch{{Name: "rules", Parent: "origin/main", Base: "b0"}}}
			long := "common/modules/inboundrulebook/rules/afterpay-us-bankruptcy.md"
			m := ui.New(context.Background(), tree, ui.Sources{
				Tree:        memoryTree{tree: tree},
				Diffs:       memoryDiffs{files: map[string][]diff.File{"rules": {file(long)}}},
				Highlighter: plainText{},
				Viewed:      memoryViewed{},
			}, defaults())
			var sized tea.Model = m
			sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 20})

			view := screen(press(settle(sized, m.Init()), "t"))

			require.Contains(t, view, "M   …/afterpay-us-bankruptcy.md")
		})

		t.Run("shows files under their directories, with a chain of single directories on one row", func(t *testing.T) {
			view := screen(startWith(nestedFiles(t)))

			requireInOrder(t, view, "common/modules/", "a/", "one.go", "two.go", "b/", "three.go", "README.md")
		})

		t.Run("a file name starts one step in from its folder name, in line with the folder names beside it", func(t *testing.T) {
			view := screen(startWith(nestedFiles(t)))

			require.Equal(t, columnOf(t, view, "a/")+2, columnOf(t, view, "one.go"))
			require.Equal(t, columnOf(t, view, "common/modules/"), columnOf(t, view, "README.md"))
		})

		t.Run("J moves through the files in the order the tree shows them", func(t *testing.T) {
			m := press(startWith(nestedFiles(t)), "enter", "enter", "J", "J", "J")

			require.Contains(t, screen(m), "README.md · 4/4")
		})

		t.Run("j stops on a folder, and the right panel then sums up that folder", func(t *testing.T) {
			view := screen(press(startWith(nestedFiles(t)), "enter", "j", "j"))

			require.Contains(t, view, "Folder · common/modules/b/")
			require.Contains(t, view, "1 file  +3 -1")
		})

		t.Run("a folder name too long for the panel loses the start of its path and keeps its count", func(t *testing.T) {
			tree := stack.Tree{Trunk: "origin/main", Branches: []stack.Branch{{Name: "rules", Parent: "origin/main", Base: "b0"}}}
			long := "common/modules/inboundrulebook/rules/"
			view := screen(startWith(modelSources{tree: tree, viewed: memoryViewed{}, files: map[string][]diff.File{
				"rules": {file(long + "afterpay.md"), file(long + "zip.md")},
			}}))

			requireRow(t, view, "\u2026/rules/", "2 files")
		})

		t.Run("every folder row says how many changed files it holds", func(t *testing.T) {
			view := screen(startWith(nestedFiles(t)))

			requireRow(t, view, "▾ common/modules/", "3 files")
			requireRow(t, view, "▾ a/", "2 files")
			requireRow(t, view, "▾ b/", "1 file")
		})

		t.Run("the panel title says how many files the branch changes", func(t *testing.T) {
			require.Contains(t, screen(startWith(nestedFiles(t))), "Files · nested · 4 files")
		})

		t.Run("a folder counts only the files that a filter leaves showing", func(t *testing.T) {
			view := screen(press(startWith(nestedFiles(t)), "enter", "/", "one", "enter"))

			requireRow(t, view, "▾ a/", "1 file")
		})

		t.Run("t shows the files as a list of paths", func(t *testing.T) {
			view := screen(press(startWith(nestedFiles(t)), "t"))

			require.Contains(t, view, "common/modules/a/one.go")
			require.NotContains(t, view, "  b/")
		})
	})

	t.Run("fold", func(t *testing.T) {
		t.Run("o on a folder hides its files, and o again shows them", func(t *testing.T) {
			folded := press(startWith(nestedFiles(t)), "enter", "j", "j", "o")

			requireRow(t, screen(folded), "▸ b/", "1 file")
			require.Contains(t, screen(press(folded, "o")), "▾ b/")
		})

		t.Run("o on a file folds the folder that holds it and selects that folder", func(t *testing.T) {
			view := screen(press(startWith(nestedFiles(t)), "enter", "o"))

			requireRow(t, view, "▸ a/", "2 files")
			require.Contains(t, view, "Folder · common/modules/a/")
		})

		t.Run("J passes over the files inside a folded folder", func(t *testing.T) {
			m := press(startWith(nestedFiles(t)), "enter", "o", "enter", "J")

			require.Contains(t, screen(m), "three.go · 3/4")
		})

		t.Run("a folder folds by itself once every file in it is viewed, and the cursor moves past it", func(t *testing.T) {
			viewed := memoryViewed{}
			src := nestedFiles(t)
			src.viewed = viewed

			view := screen(press(startWith(src), "enter", "v", "v"))

			require.True(t, viewed["common/modules/a/two.go"])
			requireRow(t, view, "▸ a/", "2 files")
			require.Contains(t, view, "three.go · 3/4")
		})

		t.Run("a folder folded on one branch stays folded on the next branch that changes it", func(t *testing.T) {
			m := press(startWith(twoBranches(t)), "enter", "o", "]")

			view := screen(m)
			require.Contains(t, view, "Files · second")
			requireRow(t, view, "▸ a/", "1 file")
		})

		t.Run("switching to a branch with the selected file inside a folded folder unfolds that folder", func(t *testing.T) {
			m := press(startWith(twoBranches(t)), "enter", "]", "o", "[")

			view := screen(m)
			require.Contains(t, view, "Files · first")
			require.Contains(t, view, "▾ a/")
			require.Contains(t, view, "one.go · 1/3")
		})
	})

	t.Run("search", func(t *testing.T) {
		t.Run("/ in the stack keeps the matching branches and the branches they sit on", func(t *testing.T) {
			view := screen(press(start(memoryViewed{}), "/", "hand"))

			require.Contains(t, view, "└─ events")
			require.Contains(t, view, "└─ handler")
			require.NotContains(t, view, "billing")
			require.Contains(t, view, "/hand")
			require.Contains(t, view, "1 of 3 branches")
		})

		t.Run("enter keeps a filter, and esc then clears it", func(t *testing.T) {
			kept := press(start(memoryViewed{}), "/", "hand", "enter")

			require.Contains(t, screen(kept), "/hand · 1 of 3 branches · esc clear")

			cleared := screen(press(kept, "esc"))
			require.Contains(t, cleared, "└─ billing")
			require.NotContains(t, cleared, "/hand")
		})

		t.Run("/ in the files keeps the matching paths, and the filter stays on the next branch", func(t *testing.T) {
			filtered := press(startWith(twoBranches(t)), "enter", "/", "one", "enter")

			require.Contains(t, screen(filtered), "1 of 3 files")
			require.NotContains(t, screen(filtered), "two.go")

			next := screen(press(filtered, "]"))
			require.Contains(t, next, "Files · second")
			require.Contains(t, next, "1 of 2 files")
			require.NotContains(t, next, "four.go")
		})

		needles := func(t *testing.T) modelSources {
			t.Helper()
			src := nestedFiles(t)
			var lines []diff.Line
			for i := 1; i <= 40; i++ {
				text := fmt.Sprintf("line %d", i)
				if i == 5 || i == 30 {
					text += " holds the needle"
				}
				lines = append(lines, diff.Line{Kind: diff.Addition, Text: text, NewNumber: i})
			}
			src.patches = map[string]diff.Patch{"common/modules/a/one.go": {Hunks: []diff.Hunk{{NewStart: 1, NewLines: 40, Lines: lines}}}}
			return src
		}

		t.Run("/ in the diff finds the text, and n moves to the next match", func(t *testing.T) {
			found := press(startWith(needles(t)), "enter", "enter", "/", "needle", "enter")

			require.Contains(t, screen(found), "/needle · 1/2")
			moved := press(found, "n")
			require.Contains(t, screen(moved), "/needle · 2/2")
			require.Contains(t, screen(moved), "line 30 holds the needle")
			require.NotContains(t, screen(press(moved, "esc")), "/needle")
		})

		t.Run("while a search is on in the diff, the footer shows n for the next match and not for the next hunk", func(t *testing.T) {
			view := screen(press(startWith(threeHunks(t)), "enter", "enter", "/", "line", "enter"))

			require.Contains(t, view, "n/N match")
			require.Contains(t, view, "^d/^u page · p hunk · J/K file")
		})

		t.Run("while a search is on in the diff, the footer keeps a hunk key that no match action takes", func(t *testing.T) {
			opts := defaults()
			require.NoError(t, opts.Keys.Set(keymap.NextMatch, []string{"x"}))

			view := screen(press(startWithOptions(threeHunks(t), opts), "enter", "enter", "/", "line", "enter"))

			require.Contains(t, view, "x/N match")
			require.Contains(t, view, "n/p hunk")
		})

		t.Run("while a search is on, a key of keys.search runs ahead of the same key in keys", func(t *testing.T) {
			opts := defaults()
			require.NoError(t, opts.Keys.Set(keymap.NextMatch, []string{"s"}))

			view := screen(press(startWithOptions(needles(t), opts), "enter", "enter", "/", "needle", "enter", "s"))

			require.Contains(t, view, "/needle · 2/2")
		})

		t.Run("N in the diff moves back to the previous match", func(t *testing.T) {
			back := press(startWith(needles(t)), "enter", "enter", "/", "needle", "enter", "n", "N")

			require.Contains(t, screen(back), "/needle · 1/2")
			require.Contains(t, screen(back), "line 5 holds the needle")
		})
	})

	t.Run("sync", func(t *testing.T) {
		startWithSyncOptions := func(sync ui.Syncer, opts ui.Options) tea.Model {
			tree, files := stackOf()
			m := ui.New(context.Background(), tree, ui.Sources{
				Tree:        memoryTree{tree: tree},
				Diffs:       memoryDiffs{files: files},
				Highlighter: plainText{},
				Viewed:      memoryViewed{},
				Sync:        sync,
			}, opts)
			var sized tea.Model = m
			sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
			return settle(sized, m.Init())
		}
		startWithSync := func(sync ui.Syncer) tea.Model {
			return startWithSyncOptions(sync, defaults())
		}
		planned := func() restack.Plan {
			tree, _ := stackOf()
			return restack.Plan{Tree: tree, NewCommits: 3, Outcomes: map[string]restack.Outcome{
				"events":  {Kind: restack.Moves, NewParent: "origin/main"},
				"handler": {Kind: restack.Conflict, NewParent: "events", Files: []string{"order.go"}},
				"billing": {Kind: restack.UpToDate, NewParent: "origin/main"},
			}}
		}

		t.Run("the footer and the keys screen show S when the sync can run", func(t *testing.T) {
			m := startWithSync(&memorySync{plan: planned()})

			require.Contains(t, screen(m), "S sync")
			require.Contains(t, screen(press(m, "?")), "fetch the trunk and show the plan of a sync")
		})

		t.Run("S shows the plan in the Stack panel with an outcome for each branch", func(t *testing.T) {
			view := screen(press(startWithSync(&memorySync{plan: planned()}), "S"))

			require.Contains(t, view, "Stack · sync plan")
			require.Contains(t, view, "origin/main  3 new commits")
			require.Regexp(t, `├─ events\s+moves onto origin/main`, view)
			require.Regexp(t, `│  └─ handler\s+stays: conflict in order.go`, view)
			require.Regexp(t, `└─ billing\s+up to date`, view)
			require.Contains(t, view, "q close")
		})

		t.Run("while the plan shows, esc closes it even when the stack has a filter", func(t *testing.T) {
			view := screen(press(startWithSync(&memorySync{plan: planned()}), "/", "hand", "enter", "S", "esc"))

			require.NotContains(t, view, "sync plan")
		})

		t.Run("while the plan shows, a key of keys.plan runs ahead of the same key in keys", func(t *testing.T) {
			opts := defaults()
			require.NoError(t, opts.Keys.Set(keymap.ClosePlan, []string{"Q"}))
			m := press(startWithSyncOptions(&memorySync{plan: planned()}, opts), "S")

			next, cmd := m.Update(keyPress("Q"))

			require.NotContains(t, screen(settle(next, cmd)), "sync plan")
		})

		t.Run("esc closes the plan, and the Stack panel shows the tree as it was before", func(t *testing.T) {
			p := planned()
			p.Tree.Branches[1].Behind = 0

			view := screen(press(startWithSync(&memorySync{plan: p}), "S", "esc"))

			require.NotContains(t, view, "sync plan")
			require.NotContains(t, view, "moves onto")
			require.Contains(t, view, "1 behind parent")
		})

		t.Run("q closes the plan", func(t *testing.T) {
			view := screen(press(startWithSync(&memorySync{plan: planned()}), "S", "q"))

			require.NotContains(t, view, "sync plan")
		})

		t.Run("while the plan shows, q in the files goes back to the Stack panel and leaves the plan open", func(t *testing.T) {
			view := screen(press(startWithSync(&memorySync{plan: planned()}), "S", "tab", "q"))

			require.Contains(t, view, "sync plan")
			require.Contains(t, view, "q close")
		})

		t.Run("when the sync cannot run, the footer and the keys screen hide S, and S shows why", func(t *testing.T) {
			m := startWithSync(&memorySync{unavailable: errors.New("sync needs git 2.44 or later, and this is git 2.43.0")})

			require.NotContains(t, screen(m), "S sync")
			require.NotContains(t, screen(press(m, "?")), "show the plan of a sync")
			require.Contains(t, screen(press(m, "S")), "sync needs git 2.44 or later, and this is git 2.43.0")
		})

		t.Run("a plan that fails shows its error in the status line", func(t *testing.T) {
			view := screen(press(startWithSync(&memorySync{planErr: errors.New("git fetch --prune origin: could not read Username")}), "S"))

			require.Contains(t, view, "could not read Username")
			require.NotContains(t, view, "sync plan")
		})

		t.Run("without a sync source, S does nothing", func(t *testing.T) {
			view := screen(press(start(memoryViewed{}), "S"))

			require.NotContains(t, view, "S sync")
			require.NotContains(t, view, "sync plan")
		})

		movable := func() restack.Plan {
			p := planned()
			p.Outcomes["handler"] = restack.Outcome{Kind: restack.Moves, NewParent: "events"}
			return p
		}
		bothMove := func() restack.Plan {
			p := movable()
			p.Outcomes["billing"] = restack.Outcome{Kind: restack.Moves, NewParent: "origin/main"}
			return p
		}
		eventsMoved := func() restack.Plan {
			tree, _ := stackOf()
			tree.Branches[1].Behind = 0
			return restack.Plan{Tree: tree, Outcomes: map[string]restack.Outcome{
				"events":  {Kind: restack.UpToDate, NewParent: "origin/main"},
				"handler": {Kind: restack.UpToDate, NewParent: "events"},
				"billing": {Kind: restack.Moves, NewParent: "origin/main"},
			}}
		}

		t.Run("while the plan shows, the footer shows enter on a branch of a stack that moves, and not on other branches", func(t *testing.T) {
			m := press(startWithSync(&memorySync{plan: movable()}), "S")

			require.Contains(t, screen(m), "⏎ move this stack")
			require.NotContains(t, screen(press(m, "j", "j")), "⏎ move this stack")
		})

		t.Run("enter moves only the stack of the selected branch", func(t *testing.T) {
			sync := &memorySync{plan: bothMove(), result: restack.Result{Moved: 1}, replanned: movable()}

			press(startWithSync(sync), "S", "j", "j", "enter")

			tree, _ := stackOf()
			require.Equal(t, []restack.Plan{{
				Tree:       stack.Tree{Trunk: "origin/main", Current: "events", Branches: tree.Branches[2:]},
				NewCommits: 3,
				Outcomes:   map[string]restack.Outcome{"billing": {Kind: restack.Moves, NewParent: "origin/main"}},
			}}, sync.moved)
		})

		t.Run("after a move, the plan stays open with the outcomes of a new plan", func(t *testing.T) {
			sync := &memorySync{plan: bothMove(), result: restack.Result{Moved: 1}, replanned: eventsMoved()}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.Contains(t, view, "Stack · sync plan")
			require.Regexp(t, `├─ events\s+up to date`, view)
			require.Regexp(t, `└─ billing\s+moves onto origin/main`, view)
		})

		t.Run("after a move, the footer names the stack that moved", func(t *testing.T) {
			sync := &memorySync{plan: bothMove(), result: restack.Result{Moved: 1}, replanned: eventsMoved()}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.Contains(t, view, "Moved the stack of events.")
		})

		t.Run("a move that moves no branch says that the stack did not move", func(t *testing.T) {
			sync := &memorySync{plan: movable(), result: restack.Result{}, replanned: movable()}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.Contains(t, view, "The stack of events did not move.")
		})

		t.Run("after a move, the plan keeps the count of new commits that the fetch got", func(t *testing.T) {
			sync := &memorySync{plan: bothMove(), result: restack.Result{Moved: 1}, replanned: eventsMoved()}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.Contains(t, view, "origin/main  3 new commits")
		})

		t.Run("closing the plan after a move shows the tree of the new plan", func(t *testing.T) {
			sync := &memorySync{plan: bothMove(), result: restack.Result{Moved: 1}, replanned: eventsMoved()}

			view := screen(press(startWithSync(sync), "S", "enter", "q"))

			require.NotContains(t, view, "sync plan")
			require.NotContains(t, view, "behind parent")
		})

		t.Run("when the new plan after a move fails, the plan closes and the status line shows why", func(t *testing.T) {
			sync := &memorySync{plan: bothMove(), result: restack.Result{Moved: 1}, replanErr: errors.New("git replay: bad object")}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.NotContains(t, view, "sync plan")
			require.Contains(t, view, "git replay: bad object")
		})

		t.Run("enter on a stack that does not move moves nothing, and says so", func(t *testing.T) {
			sync := &memorySync{plan: movable()}

			view := screen(press(startWithSync(sync), "S", "j", "j", "enter"))

			require.Empty(t, sync.moved)
			require.Contains(t, view, "the stack of billing does not move")
		})

		t.Run("when the plan is closed, enter goes to the files as before", func(t *testing.T) {
			sync := &memorySync{plan: movable()}

			view := screen(press(startWithSync(sync), "S", "esc", "enter"))

			require.Empty(t, sync.moved)
			require.Contains(t, view, "o fold")
		})

		t.Run("while the plan shows, enter in the files goes to the diff and moves nothing", func(t *testing.T) {
			sync := &memorySync{plan: movable()}

			view := screen(press(startWithSync(sync), "S", "tab", "enter"))

			require.Empty(t, sync.moved)
			require.Contains(t, view, "j/k scroll")
		})

		t.Run("a move that fails shows its error in the status line", func(t *testing.T) {
			sync := &memorySync{plan: movable(), moveErr: errors.New("make the commits of the sync: gpg failed to sign the data")}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.Contains(t, view, "gpg failed to sign the data")
		})

		t.Run("a stack that stays at the move shows why, with the command that finishes the move", func(t *testing.T) {
			sync := &memorySync{plan: movable(), replanned: movable(), result: restack.Result{Stayed: []restack.Stay{{
				Stack:  "events",
				Reason: "handler did not move in /work/handler. To finish the move, run: git -C /work/handler reset --keep refs/strata/sync/handler",
			}}}}

			view := screen(press(startWithSync(sync), "S", "enter"))

			require.Contains(t, view, "git -C /work/handler reset --keep refs/strata/sync/handler")
		})

		t.Run("while the plan is on its way, the footer shows the step that the plan is at", func(t *testing.T) {
			sync := steppingSync{memorySync: &memorySync{plan: planned()}, step: "replaying handler (2 of 3)",
				reported: make(chan struct{}), release: make(chan struct{})}
			m, cmd := startWithSync(sync).Update(keyPress("S"))
			batch, ok := cmd().(tea.BatchMsg)
			require.True(t, ok)
			msgs := make(chan tea.Msg, len(batch))
			for _, c := range batch {
				go func() { msgs <- c() }()
			}
			<-sync.reported

			m, _ = m.Update(<-msgs)
			close(sync.release)

			require.Contains(t, screen(m), "planning the sync: replaying handler (2 of 3)…")
		})

		t.Run("the keys screen lists enter and esc for the plan", func(t *testing.T) {
			view := screen(press(startWithSync(&memorySync{plan: movable()}), "?"))

			require.Contains(t, view, "move the stack of the selected branch")
			require.Contains(t, view, "close the plan")
			require.Contains(t, view, "resolve the conflict of the stack in a shell")
		})

		t.Run("while the plan shows, the footer shows c on a branch of a stack with a conflict, and not on other branches", func(t *testing.T) {
			m := press(startWithSync(&memorySync{plan: planned()}), "S")

			require.Contains(t, screen(m), "c resolve the conflict")
			require.NotContains(t, screen(press(m, "j", "j")), "c resolve the conflict")
		})

		t.Run("c starts the sync rebase for the stack of the selected branch", func(t *testing.T) {
			sync := &memorySync{plan: planned()}

			press(startWithSync(sync), "S", "j", "c")

			require.Equal(t, []string{"handler"}, sync.resolved)
		})

		t.Run("while the plan shows, c in the files starts no sync rebase", func(t *testing.T) {
			sync := &memorySync{plan: planned()}

			press(startWithSync(sync), "S", "j", "tab", "c")

			require.Empty(t, sync.resolved)
		})

		t.Run("c on a stack with no conflict does nothing", func(t *testing.T) {
			sync := &memorySync{plan: planned()}

			press(startWithSync(sync), "S", "j", "j", "c")

			require.Empty(t, sync.resolved)
		})

		t.Run("S while a sync rebase waits says how to finish it", func(t *testing.T) {
			waits := restack.Pending{State: restack.RebaseWaits, Stack: "events", Worktree: "/work/sync"}

			view := screen(press(startWithSync(&memorySync{plan: planned(), pending: waits}), "S"))

			require.Contains(t, view, "git rebase --continue")
			require.NotContains(t, view, "sync plan")
		})

		t.Run("S with a done sync rebase shows the plan of the resolved stack, and enter moves it", func(t *testing.T) {
			tree, _ := stackOf()
			resolved := restack.Plan{Tree: stack.Tree{Trunk: tree.Trunk, Branches: tree.Branches[:2]}, Outcomes: map[string]restack.Outcome{
				"events":  {Kind: restack.Moves, NewParent: "origin/main", NewTip: "e2"},
				"handler": {Kind: restack.Moves, NewParent: "events", NewTip: "h2"},
			}}
			sync := &memorySync{
				plan:      planned(),
				pending:   restack.Pending{State: restack.RebaseDone, Stack: "events", Plan: resolved},
				result:    restack.Result{Moved: 1},
				replanned: eventsMoved(),
			}

			m := press(startWithSync(sync), "S")

			require.Regexp(t, `└─ events\s+moves onto origin/main`, screen(m))
			require.NotContains(t, screen(m), "billing")
			require.Contains(t, screen(m), "⏎ move the resolved stack")
			view := screen(press(m, "enter"))
			require.Equal(t, 1, sync.finished)
			require.Empty(t, sync.moved)
			require.Contains(t, view, "Moved the stack of events.")
		})

		t.Run("S after an aborted sync rebase removes what it left, and plans as usual", func(t *testing.T) {
			sync := &memorySync{plan: planned(), pending: restack.Pending{State: restack.RebaseAborted, Stack: "events"}}

			view := screen(press(startWithSync(sync), "S"))

			require.Equal(t, 1, sync.finished)
			require.Contains(t, view, "sync plan")
			require.Contains(t, view, "You aborted the sync rebase for the stack of events, and no branch moved.")
		})

		t.Run("S does nothing while a plan is on its way", func(t *testing.T) {
			m, first := startWithSync(&memorySync{plan: planned()}).Update(keyPress("S"))
			_, second := m.Update(keyPress("S"))

			require.NotNil(t, first)
			require.Nil(t, second)
		})

		t.Run("enter does nothing while a move is on its way", func(t *testing.T) {
			m := press(startWithSync(&memorySync{plan: movable()}), "S")
			m, first := m.Update(keyPress("enter"))
			_, second := m.Update(keyPress("enter"))

			require.NotNil(t, first)
			require.Nil(t, second)
		})

		t.Run("c does nothing while a sync rebase is on its way", func(t *testing.T) {
			m := press(startWithSync(&memorySync{plan: planned()}), "S")
			m, first := m.Update(keyPress("c"))
			_, second := m.Update(keyPress("c"))

			require.NotNil(t, first)
			require.Nil(t, second)
		})
	})

	t.Run("diff", func(t *testing.T) {

		t.Run("n goes to the next hunk", func(t *testing.T) {
			view := screen(press(startWith(threeHunks(t)), "enter", "enter", "n"))

			require.Contains(t, view, "@@ -100,30 +100,30 @@")
			require.NotContains(t, view, "@@ -1,30 +1,30 @@")
		})

		t.Run("p goes back to the previous hunk", func(t *testing.T) {
			view := screen(press(startWith(threeHunks(t)), "enter", "enter", "n", "p"))

			require.Contains(t, view, "@@ -1,30 +1,30 @@")
			require.NotContains(t, view, "@@ -100,30 +100,30 @@")
		})

		t.Run("colours the diff with the highlighter's theme", func(t *testing.T) {
			tree, files := stackOf()
			m := ui.New(context.Background(), tree, ui.Sources{
				Tree:        memoryTree{tree: tree},
				Diffs:       memoryDiffs{files: files},
				Highlighter: plainText{theme: syntax.Theme{Inserted: syntax.Color{R: 10, G: 200, B: 30, Set: true}}},
				Viewed:      memoryViewed{},
			}, defaults())
			var sized tea.Model = m
			sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})

			view := press(settle(sized, m.Init()), "enter", "s").View().Content

			require.Contains(t, view, "38;2;10;200;30")
		})

		t.Run("tints the diff with colours of the 256-colour palette when the terminal has only those", func(t *testing.T) {
			tree, files := stackOf()
			m := ui.New(context.Background(), tree, ui.Sources{
				Tree:        memoryTree{tree: tree},
				Diffs:       memoryDiffs{files: files},
				Highlighter: plainText{},
				Viewed:      memoryViewed{},
			}, defaults())
			var sized tea.Model = m
			sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
			profiled, _ := settle(sized, m.Init()).Update(tea.ColorProfileMsg{Profile: colorprofile.ANSI256})

			view := press(profiled, "enter").View().Content

			at := strings.Index(view, "content of events.go on events")
			require.NotEqual(t, -1, at)
			backgrounds := regexp.MustCompile(`48;2;(\d+);(\d+);(\d+)`).FindAllStringSubmatch(view[:at], -1)
			require.NotEmpty(t, backgrounds)
			require.Subset(t, []string{"0", "95", "135", "175", "215", "255"}, backgrounds[len(backgrounds)-1][1:])
		})
	})

	t.Run("options", func(t *testing.T) {
		withKeys := func(t *testing.T, a keymap.Action, keys ...string) ui.Options {
			t.Helper()
			opts := defaults()
			require.NoError(t, opts.Keys.Set(a, keys))
			return opts
		}

		t.Run("a key that the options give an action runs that action", func(t *testing.T) {
			m := startWithOptions(threeHunks(t), withKeys(t, keymap.NextHunk, "ctrl+n"))

			view := screen(press(m, "enter", "enter", "ctrl+n"))

			require.Contains(t, view, "@@ -100,30 +100,30 @@")
		})

		t.Run("a default key that the options take from an action does nothing", func(t *testing.T) {
			m := startWithOptions(threeHunks(t), withKeys(t, keymap.NextHunk, "ctrl+n"))

			view := screen(press(m, "enter", "enter", "n"))

			require.Contains(t, view, "@@ -1,30 +1,30 @@")
		})

		t.Run("the keys screen shows each action with all the keys that the options give it", func(t *testing.T) {
			m := startWithOptions(threeHunks(t), withKeys(t, keymap.NextHunk, "ctrl+n", "x"))

			view := screen(press(m, "?"))

			require.Regexp(t, `ctrl\+n  x +next hunk`, view)
		})

		t.Run("the footer shows the first key of each action", func(t *testing.T) {
			m := startWithOptions(threeHunks(t), withKeys(t, keymap.NextHunk, "ctrl+n", "n"))

			view := screen(press(m, "enter", "enter"))

			require.Contains(t, view, "^n/p hunk")
		})

		t.Run("the footer leaves out an action that has no key", func(t *testing.T) {
			opts := withKeys(t, keymap.NextHunk)
			require.NoError(t, opts.Keys.Set(keymap.PreviousHunk, nil))

			view := screen(press(startWithOptions(threeHunks(t), opts), "enter", "enter"))

			require.Contains(t, view, "^d/^u page · J/K file")
		})

		t.Run("a key of a match action runs the action of the files panel while the files have a filter", func(t *testing.T) {
			m := startWithOptions(nestedFiles(t), withKeys(t, keymap.NextMatch, "j"))

			view := screen(press(m, "enter", "t", "/", ".go", "enter", "j"))

			require.Contains(t, view, "common/modules/a/two.go · 3/4")
		})

		t.Run("ctrl+c quits when the options leave quit with no key", func(t *testing.T) {
			m := startWithOptions(threeHunks(t), withKeys(t, keymap.Quit))

			_, cmd := m.Update(keyPress("ctrl+c"))

			require.NotNil(t, cmd)
			require.Equal(t, tea.QuitMsg{}, cmd())
		})

		t.Run("split off starts with the unified diff that s shows", func(t *testing.T) {
			unified := defaults()
			unified.Split = false

			started := screen(press(startWithOptions(nestedFiles(t), unified), "enter"))

			require.Equal(t, screen(press(startWith(nestedFiles(t)), "enter", "s")), started)
		})
	})

	t.Run("keys screen", func(t *testing.T) {
		t.Run("keeps two columns on a narrow screen that is too short for one column", func(t *testing.T) {
			m, _ := startWith(threeHunks(t)).Update(tea.WindowSizeMsg{Width: 100, Height: 40})

			view := screen(press(m, "?"))

			require.Regexp(t, `N +previous match`, view)
		})

		t.Run("shows one column with nothing cut off on a narrow screen tall enough for it", func(t *testing.T) {
			m, _ := startWith(threeHunks(t)).Update(tea.WindowSizeMsg{Width: 100, Height: 80})

			view := screen(press(m, "?"))

			require.Contains(t, view, "mark the file viewed, then go to the next file")
		})

		t.Run("draws on a screen too narrow for any column", func(t *testing.T) {
			for width := 1; width <= 12; width++ {
				m, _ := startWith(threeHunks(t)).Update(tea.WindowSizeMsg{Width: width, Height: 10})

				require.NotPanics(t, func() { screen(press(m, "?")) }, "width %d", width)
			}
		})
	})

	t.Run("zoom", func(t *testing.T) {
		t.Run("gives the diff the whole screen, and esc returns to the panels", func(t *testing.T) {
			zoomed := press(start(memoryViewed{}), "enter", "z")

			require.NotContains(t, screen(zoomed), "Stack")
			require.Contains(t, screen(zoomed), "content of events.go on events")

			back := press(zoomed, "esc")
			require.Contains(t, screen(back), "Stack")
		})
	})

	t.Run("back", func(t *testing.T) {
		t.Run("q in the files goes back to the Stack panel", func(t *testing.T) {
			view := screen(press(start(memoryViewed{}), "enter", "q"))

			require.Contains(t, view, "⏎ files")
		})

		t.Run("q in the diff goes back to the files", func(t *testing.T) {
			view := screen(press(start(memoryViewed{}), "enter", "enter", "q"))

			require.Contains(t, view, "o fold")
		})
	})

	t.Run("quit", func(t *testing.T) {
		t.Run("Q quits from the diff", func(t *testing.T) {
			m := press(start(memoryViewed{}), "enter", "enter")

			_, cmd := m.Update(keyPress("Q"))

			require.NotNil(t, cmd)
			require.Equal(t, tea.QuitMsg{}, cmd())
		})

		t.Run("q in the Stack panel does not quit", func(t *testing.T) {
			_, cmd := start(memoryViewed{}).Update(keyPress("q"))

			require.Nil(t, cmd)
		})
	})

	t.Run("delete", func(t *testing.T) {
		tipped := func() stack.Tree {
			tree, _ := stackOf()
			for i, tip := range []string{"e0e0e0e0e0", "a1a1a1a1a1", "b2b2b2b2b2"} {
				tree.Branches[i].Tip = tip
			}
			return tree
		}
		startWithDeleter := func(deleter ui.Deleter, before, after stack.Tree) tea.Model {
			_, files := stackOf()
			m := ui.New(context.Background(), before, ui.Sources{
				Tree:        memoryTree{tree: after},
				Diffs:       memoryDiffs{files: files},
				Highlighter: plainText{},
				Viewed:      memoryViewed{},
				Deleter:     deleter,
			}, defaults())
			var sized tea.Model = m
			sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
			return settle(sized, m.Init())
		}
		startDeleting := func(deleter ui.Deleter) tea.Model {
			return startWithDeleter(deleter, tipped(), tipped())
		}

		t.Run("the footer shows space and d when strata can delete", func(t *testing.T) {
			view := screen(startDeleting(&memoryDeleter{}))

			require.Contains(t, view, "space mark · d delete")
		})

		t.Run("without a deleter, the footer hides mark and delete, and d does nothing", func(t *testing.T) {
			m := start(memoryViewed{})

			require.NotContains(t, screen(m), "d delete")
			require.NotContains(t, screen(press(m, "j", "j", "d")), "Stack · delete")
		})

		t.Run("space marks the branch, and space again unmarks it", func(t *testing.T) {
			marked := press(startDeleting(&memoryDeleter{}), "j", "j", "space")

			require.Regexp(t, `●└─ billing`, screen(marked))
			require.Contains(t, screen(marked), "1 marked")
			require.NotContains(t, screen(press(marked, "space")), "●")
		})

		t.Run("a marked branch stays in the Stack panel when a filter hides the other branches", func(t *testing.T) {
			view := screen(press(startDeleting(&memoryDeleter{}), "j", "j", "space", "/", "hand", "enter"))

			require.Regexp(t, `●└─ billing`, view)
		})

		t.Run("d shows what the selected branch loses, and deletes nothing yet", func(t *testing.T) {
			deleter := &memoryDeleter{}

			view := screen(press(startDeleting(deleter), "j", "j", "d"))

			require.Regexp(t, `└─ billing\s+loses 1 commit`, view)
			require.Contains(t, view, "y delete 1 branch · any other key cancels")
			require.Empty(t, deleter.deleted)
		})

		t.Run("d with marked branches asks to delete all of them", func(t *testing.T) {
			view := screen(press(startDeleting(&memoryDeleter{}), "space", "j", "space", "d"))

			require.Contains(t, view, "Stack · delete 2 branches")
			require.Regexp(t, `├─ events\s+loses 2 commits`, view)
			require.Regexp(t, `│  └─ handler\s+loses 1 commit`, view)
		})

		t.Run("the delete says when the trunk has the changes of a branch", func(t *testing.T) {
			view := screen(press(startDeleting(&memoryDeleter{deletions: map[string]stack.Deletion{"billing": {TrunkHas: true}}}), "j", "j", "d"))

			require.Regexp(t, `└─ billing\s+the trunk has its changes`, view)
		})

		t.Run("the delete of a branch in another worktree says that it removes the worktree", func(t *testing.T) {
			deleter := &memoryDeleter{deletions: map[string]stack.Deletion{"billing": {RemovesWorktree: "/work/strata-billing"}}}

			view := screen(press(startDeleting(deleter), "j", "j", "d"))

			require.Regexp(t, `└─ billing\s+loses 1 commit · removes worktree strata-billing`, view)
			require.Contains(t, view, "y delete 1 branch and remove 1 worktree · any other key cancels")
		})

		t.Run("the delete lists the changed files that the removed worktree loses", func(t *testing.T) {
			deleter := &memoryDeleter{deletions: map[string]stack.Deletion{"billing": {
				RemovesWorktree: "/work/strata-billing", LostFiles: []string{"invoice.go", "notes.txt"},
			}}}

			view := screen(press(startDeleting(deleter), "j", "j", "d"))

			require.Contains(t, view, "removes worktree strata-billing and loses 2 changed files: invoice.go, notes.txt")
			require.Contains(t, view, "y delete 1 branch, remove 1 worktree and lose 2 changed files · any other key cancels")
		})

		t.Run("the delete of a worktree whose folder is gone says that strata forgets it", func(t *testing.T) {
			deleter := &memoryDeleter{deletions: map[string]stack.Deletion{"billing": {RemovesWorktree: "/work/strata-billing", WorktreeGone: true}}}

			view := screen(press(startDeleting(deleter), "j", "j", "d"))

			require.Contains(t, view, "forgets worktree strata-billing, whose folder is gone")
		})

		t.Run("the delete names the windows of the tmux panes that it closes", func(t *testing.T) {
			deleter := &memoryDeleter{deletions: map[string]stack.Deletion{"billing": {
				RemovesWorktree: "/work/strata-billing",
				ClosesPanes: []tmux.Pane{
					{ID: "%1", Window: "work:strata-billing", Path: "/work/strata-billing"},
					{ID: "%2", Window: "work:strata-billing", Path: "/work/strata-billing/invoices"},
					{ID: "%3", Window: "work:edit", Path: "/work/strata-billing"},
				},
			}}}

			view := screen(press(startDeleting(deleter), "j", "j", "d"))

			require.Contains(t, view, "removes worktree strata-billing · closes 3 tmux panes in work:strata-billing and work:edit")
			require.Contains(t, view, "y delete 1 branch, remove 1 worktree and close 3 tmux panes · any other key cancels")
		})

		t.Run("a check that refuses the delete shows why in the status line", func(t *testing.T) {
			deleter := &memoryDeleter{checkErr: errors.New("billing is checked out here: switch to another branch first")}

			view := screen(press(startDeleting(deleter), "j", "j", "d"))

			require.Contains(t, view, "billing is checked out here: switch to another branch first")
			require.NotContains(t, view, "Stack · delete")
		})

		t.Run("after the delete, the footer names each removed worktree", func(t *testing.T) {
			deleter := &memoryDeleter{deletions: map[string]stack.Deletion{"billing": {RemovesWorktree: "/work/strata-billing"}}}

			view := screen(press(startDeleting(deleter), "j", "j", "d", "y"))

			require.Contains(t, view, "Deleted billing (was b2b2b2b). Removed worktree strata-billing.")
		})

		t.Run("d refuses a branch that another branch sits on", func(t *testing.T) {
			view := screen(press(startDeleting(&memoryDeleter{}), "d"))

			require.Contains(t, view, "handler sits on events: mark handler too")
			require.NotContains(t, view, "Stack · delete")
		})

		t.Run("y deletes the marked branches together", func(t *testing.T) {
			deleter := &memoryDeleter{}

			press(startDeleting(deleter), "space", "j", "space", "d", "y")

			require.Equal(t, [][]string{{"events", "handler"}}, deleter.deleted)
		})

		t.Run("any other key than y cancels the delete", func(t *testing.T) {
			deleter := &memoryDeleter{}

			view := screen(press(startDeleting(deleter), "j", "j", "d", "n"))

			require.Empty(t, deleter.deleted)
			require.NotContains(t, view, "Stack · delete")
		})

		t.Run("after the delete, the footer names each deleted branch with its tip", func(t *testing.T) {
			view := screen(press(startDeleting(&memoryDeleter{}), "space", "j", "space", "d", "y"))

			require.Contains(t, view, "Deleted events (was e0e0e0e) and handler (was a1a1a1a).")
		})

		t.Run("after the delete, the Stack panel reads the branches again", func(t *testing.T) {
			after := tipped()
			after.Branches = after.Branches[:2]

			view := screen(press(startWithDeleter(&memoryDeleter{}, tipped(), after), "j", "j", "d", "y"))

			require.NotRegexp(t, `└─ billing`, view)
		})

		t.Run("a delete that fails shows why in the status line", func(t *testing.T) {
			deleter := &memoryDeleter{err: errors.New("strata deleted no branch: cannot lock ref 'refs/heads/billing'")}

			view := screen(press(startDeleting(deleter), "j", "j", "d", "y"))

			require.Contains(t, view, "strata deleted no branch: cannot lock ref")
		})
	})
}
