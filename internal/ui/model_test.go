//go:build unit

package ui_test

import (
	"context"
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
	files map[string][]diff.File
}

func (m memoryDiffs) Files(_ context.Context, _, branch string) ([]diff.File, error) {
	return m.files[branch], nil
}

func (m memoryDiffs) Patch(_ context.Context, _, branch string, f diff.File) (diff.Patch, error) {
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
		return diff.File{Path: path, Status: diff.Modified, OldBlob: "old-" + path, NewBlob: "new-" + path}
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

			view := screen(settle(sized, m.Init()))

			require.Contains(t, view, "M   …/afterpay-us-bankruptcy.md")
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
