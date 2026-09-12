//go:build integration

package stack_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/stretchr/testify/require"
)

func TestReader(t *testing.T) {
	read := func(t *testing.T, repo *gittest.Repo, patterns ...string) stack.Tree {
		t.Helper()
		if len(patterns) == 0 {
			patterns = []string{"refs/heads/"}
		}
		tree, err := stack.NewReader(git.New(repo.Dir), "origin/main", patterns).Read(context.Background())
		require.NoError(t, err)
		return tree
	}
	branch := func(t *testing.T, tree stack.Tree, name string) stack.Branch {
		t.Helper()
		i := tree.Index(name)
		require.NotEqual(t, -1, i, "branch %s is not in the tree", name)
		return tree.Branches[i]
	}
	names := func(tree stack.Tree) []string {
		var out []string
		for _, b := range tree.Branches {
			out = append(out, b.Name)
		}
		return out
	}

	t.Run("read", func(t *testing.T) {
		t.Run("a branch stacked on another sits on it and counts only its own commits", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")

			tree := read(t, repo)

			require.Equal(t, []string{"events", "handler"}, names(tree))
			events, handler := branch(t, tree, "events"), branch(t, tree, "handler")
			require.Equal(t, "origin/main", events.Parent)
			require.Equal(t, 0, events.Level)
			require.Equal(t, 2, events.Commits)
			require.Equal(t, "events", handler.Parent)
			require.Equal(t, 1, handler.Level)
			require.Equal(t, 1, handler.Commits)
			require.Equal(t, 0, handler.Behind)
		})

		t.Run("separate stacks each sit on the trunk", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("orders")
			repo.Commit("orders.go", "package orders\n", "Add orders")
			repo.Switch("main")
			repo.SwitchNew("billing")
			repo.Commit("billing.go", "package billing\n", "Add billing")
			repo.SwitchNew("billing-pdf")
			repo.Commit("pdf.go", "package billing\n", "Add the PDF")

			tree := read(t, repo)

			require.Equal(t, []string{"billing", "billing-pdf", "orders"}, names(tree))
			require.Equal(t, "origin/main", branch(t, tree, "orders").Parent)
			require.Equal(t, "billing", branch(t, tree, "billing-pdf").Parent)
		})

		t.Run("a branch merged into the trunk is left out", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("done")
			repo.Commit("done.go", "package done\n", "Finish")
			repo.Switch("main")
			repo.Git("merge", "-q", "--ff-only", "done")
			repo.Git("push", "-q", "origin", "main")
			repo.SwitchNew("open")
			repo.Commit("open.go", "package open\n", "Start")

			require.Equal(t, []string{"open"}, names(read(t, repo)))
		})

		t.Run("a branch created from its parent by name still sits on it after the parent moves on", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("parent")
			repo.Commit("parent.go", "package parent\n", "Start the parent")
			repo.Git("switch", "-q", "-c", "child", "parent")
			repo.Commit("child.go", "package child\n", "Start the child")
			repo.Switch("parent")
			repo.Commit("parent.go", "package parent\n\n// fixed\n", "Fix the parent")

			child := branch(t, read(t, repo), "child")

			require.Equal(t, "parent", child.Parent)
			require.Equal(t, 1, child.Commits)
			require.Equal(t, 1, child.Behind)
		})

		t.Run("a branch created with checkout -b still sits on the branch it was created from after that branch moves on", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("parent")
			repo.Commit("parent.go", "package parent\n", "Start the parent")
			repo.SwitchNew("child")
			repo.Commit("child.go", "package child\n", "Start the child")
			repo.Switch("parent")
			repo.Commit("parent.go", "package parent\n\n// fixed\n", "Fix the parent")

			child := branch(t, read(t, repo), "child")

			require.Equal(t, "parent", child.Parent)
			require.Equal(t, 1, child.Commits)
			require.Equal(t, 1, child.Behind)
		})

		t.Run("a branch created from its parent's commit in a worktree still sits on it after the parent moves on", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("parent")
			repo.Commit("parent.go", "package parent\n", "Start the parent")
			worktree := repo.WorktreeAtCommit("child", "parent")
			worktree.Commit("child.go", "package child\n", "Start the child")
			repo.Commit("parent.go", "package parent\n\n// fixed\n", "Fix the parent")

			child := branch(t, read(t, repo), "child")

			require.Equal(t, "parent", child.Parent)
			require.Equal(t, 1, child.Commits)
			require.Equal(t, 1, child.Behind)
		})

		t.Run("an empty branch left behind by its parent sits on that parent, and the parent keeps its own parent", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("base")
			repo.Commit("base.go", "package base\n", "Start the base")
			repo.SwitchNew("parent")
			repo.Commit("parent.go", "package parent\n", "Start the parent")
			repo.Git("branch", "empty", "parent")
			repo.Commit("parent.go", "package parent\n\n// more\n", "Continue the parent")

			tree := read(t, repo)

			require.Equal(t, "base", branch(t, tree, "parent").Parent)
			empty := branch(t, tree, "empty")
			require.Equal(t, "parent", empty.Parent)
			require.Equal(t, 0, empty.Commits)
			require.Equal(t, 1, empty.Behind)
		})

		t.Run("a branch sharing its tip with the branch it was created from sits on it after both were rewritten", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("parent")
			repo.Commit("parent.go", "package parent\n", "Start the parent")
			repo.SwitchNew("child")
			repo.Switch("parent")
			repo.Git("commit", "-q", "--amend", "-m", "Reword the parent")
			repo.Git("branch", "-f", "child", "parent")

			tree := read(t, repo)

			require.Equal(t, "origin/main", branch(t, tree, "parent").Parent)
			child := branch(t, tree, "child")
			require.Equal(t, "parent", child.Parent)
			require.Equal(t, 0, child.Commits)
		})

		t.Run("counts the files and lines a branch changes against its parent", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("parent")
			repo.Commit("a.go", "one\ntwo\n", "Add a")
			repo.SwitchNew("child")
			repo.Write("a.go", "one\nthree\nfour\n")
			repo.Commit("b.go", "b\n", "Change a and add b")

			child := branch(t, read(t, repo), "child")

			require.Equal(t, 2, child.Files)
			require.Equal(t, 3, child.Insertions)
			require.Equal(t, 1, child.Deletions)
		})

		t.Run("gives the commit at the tip of each branch", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")

			events := branch(t, read(t, repo), "events")

			require.Equal(t, strings.TrimSpace(repo.Git("rev-parse", "events")), events.Tip)
		})

		t.Run("gives the worktree that has the branch checked out", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			worktree := repo.Worktree("events")
			worktreeDir, err := filepath.EvalSymlinks(worktree.Dir)
			require.NoError(t, err)

			tree := read(t, repo)

			require.Equal(t, worktreeDir, branch(t, tree, "events").Worktree)
			require.Empty(t, branch(t, tree, "handler").Worktree)
		})

		t.Run("marks a branch whose remote branch the remote deleted", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Git("push", "-q", "-u", "origin", "events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Git("push", "-q", "-u", "origin", "handler")
			repo.Git("push", "-q", "origin", "--delete", "events")

			tree := read(t, repo)

			require.True(t, branch(t, tree, "events").UpstreamGone)
			require.False(t, branch(t, tree, "handler").UpstreamGone)
		})

		t.Run("names the checked-out branch as current", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("first")
			repo.Commit("first.go", "package first\n", "Start first")
			repo.SwitchNew("second")
			repo.Commit("second.go", "package second\n", "Start second")
			repo.Switch("first")

			require.Equal(t, "first", read(t, repo).Current)
		})

		t.Run("patterns limit the tree to matching branches", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("team/one")
			repo.Commit("one.go", "package one\n", "Start one")
			repo.Switch("main")
			repo.SwitchNew("other")
			repo.Commit("other.go", "package other\n", "Start other")

			require.Equal(t, []string{"team/one"}, names(read(t, repo, "refs/heads/team/")))
		})
	})

	t.Run("commits", func(t *testing.T) {
		t.Run("lists the branch's own commits, newest first", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("parent")
			repo.Commit("parent.go", "package parent\n", "Start the parent")
			repo.SwitchNew("child")
			repo.Commit("child.go", "package child\n", "Start the child")
			repo.Commit("child.go", "package child\n\n// more\n", "Continue the child")
			reader := stack.NewReader(git.New(repo.Dir), "origin/main", []string{"refs/heads/"})
			tree, err := reader.Read(context.Background())
			require.NoError(t, err)

			commits, err := reader.Commits(context.Background(), branch(t, tree, "child"))

			require.NoError(t, err)
			require.Len(t, commits, 2)
			require.Equal(t, "Continue the child", commits[0].Subject)
			require.Equal(t, "Start the child", commits[1].Subject)
		})
	})
}

func TestFindTrunk(t *testing.T) {
	t.Run("uses the remote's default branch", func(t *testing.T) {
		repo := gittest.New(t)

		trunk, err := stack.FindTrunk(context.Background(), git.New(repo.Dir))

		require.NoError(t, err)
		require.Equal(t, "origin/main", trunk)
	})

	t.Run("falls back to the local main branch without a remote", func(t *testing.T) {
		repo := gittest.New(t)
		repo.Git("remote", "remove", "origin")

		trunk, err := stack.FindTrunk(context.Background(), git.New(repo.Dir))

		require.NoError(t, err)
		require.Equal(t, "main", trunk)
	})
}
