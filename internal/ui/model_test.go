//go:build unit

package ui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/hpcsc/strata/internal/diff"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/syntax"
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

type plainText struct{}

func (plainText) Lines(string, string) [][]syntax.Span {
	return nil
}

type memoryViewed map[string]bool

func (m memoryViewed) Has(f diff.File) bool {
	return m[f.Path]
}

func (m memoryViewed) Toggle(f diff.File) error {
	m[f.Path] = !m[f.Path]
	return nil
}

func TestModel(t *testing.T) {
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
	startWith := func(src modelSources) tea.Model {
		m := ui.New(context.Background(), src.tree, ui.Sources{
			Tree:        memoryTree{tree: src.tree},
			Diffs:       memoryDiffs{files: src.files, patches: src.patches},
			Highlighter: plainText{},
			Viewed:      src.viewed,
		})
		var sized tea.Model = m
		sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
		return settle(sized, m.Init())
	}
	start := func(viewed memoryViewed) tea.Model {
		tree, files := stackOf()
		m := ui.New(context.Background(), tree, ui.Sources{
			Tree:        memoryTree{tree: tree, commits: map[string][]stack.Commit{"events": {{Hash: "abc1234", Subject: "Name the events", Age: "2 hours ago"}}}},
			Diffs:       memoryDiffs{files: files},
			Highlighter: plainText{},
			Viewed:      viewed,
		})
		var sized tea.Model = m
		sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
		return settle(sized, m.Init())
	}
	press := func(m tea.Model, keys ...string) tea.Model {
		for _, key := range keys {
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
			switch key {
			case "enter":
				msg = tea.KeyMsg{Type: tea.KeyEnter}
			case "tab":
				msg = tea.KeyMsg{Type: tea.KeyTab}
			case "esc":
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			}
			var cmd tea.Cmd
			m, cmd = m.Update(msg)
			m = settle(m, cmd)
		}
		return m
	}
	screen := func(m tea.Model) string {
		return ansi.Strip(m.View())
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
			})
			var sized tea.Model = m
			sized, _ = sized.Update(tea.WindowSizeMsg{Width: 140, Height: 20})

			view := screen(press(settle(sized, m.Init()), "t"))

			require.Contains(t, view, "M   …/afterpay-us-bankruptcy.md")
		})

		t.Run("shows files under their directories, with a chain of single directories on one row", func(t *testing.T) {
			view := screen(startWith(nestedFiles(t)))

			requireInOrder(t, view, "common/modules/", "a/", "one.go", "two.go", "b/", "three.go", "README.md")
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

		t.Run("t shows the files as a list of paths", func(t *testing.T) {
			view := screen(press(startWith(nestedFiles(t)), "t"))

			require.Contains(t, view, "common/modules/a/one.go")
			require.NotContains(t, view, "  b/")
		})
	})

	t.Run("fold", func(t *testing.T) {
		t.Run("o on a folder hides its files, and o again shows them", func(t *testing.T) {
			folded := press(startWith(nestedFiles(t)), "enter", "j", "j", "o")

			require.Contains(t, screen(folded), "▸ b/  1 file")
			require.Contains(t, screen(press(folded, "o")), "▾ b/")
		})

		t.Run("o on a file folds the folder that holds it and selects that folder", func(t *testing.T) {
			view := screen(press(startWith(nestedFiles(t)), "enter", "o"))

			require.Contains(t, view, "▸ a/  2 files")
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
			require.Contains(t, view, "▸ a/  2 files")
			require.Contains(t, view, "three.go · 3/4")
		})

		t.Run("a folder folded on one branch stays folded on the next branch that changes it", func(t *testing.T) {
			m := press(startWith(twoBranches(t)), "enter", "o", "]")

			view := screen(m)
			require.Contains(t, view, "Files · second")
			require.Contains(t, view, "▸ a/  1 file")
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

		t.Run("/ in the diff finds the text, and n moves to the next match", func(t *testing.T) {
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

			found := press(startWith(src), "enter", "enter", "/", "needle", "enter")

			require.Contains(t, screen(found), "/needle · 1/2")
			moved := press(found, "n")
			require.Contains(t, screen(moved), "/needle · 2/2")
			require.Contains(t, screen(moved), "line 30 holds the needle")
			require.NotContains(t, screen(press(moved, "esc")), "/needle")
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
}
