//go:build integration

package stack_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/stretchr/testify/require"
)

func TestDeleter(t *testing.T) {
	ctx := context.Background()
	read := func(t *testing.T, repo *gittest.Repo) stack.Tree {
		t.Helper()
		tree, err := stack.NewReader(git.New(repo.Dir), "origin/main", []string{"refs/heads/"}).Read(ctx)
		require.NoError(t, err)
		return tree
	}
	heads := func(t *testing.T, repo *gittest.Repo) []string {
		t.Helper()
		return strings.Fields(repo.Git("for-each-ref", "--format=%(refname:short)", "refs/heads/"))
	}
	deleter := func(repo *gittest.Repo) *stack.Deleter {
		return stack.NewDeleter(git.New(repo.Dir), "origin/main")
	}

	t.Run("delete", func(t *testing.T) {
		t.Run("deletes each branch and its settings", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Git("push", "-q", "-u", "origin", "events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")

			err := deleter(repo).Delete(ctx, read(t, repo).Branches)

			require.NoError(t, err)
			require.Equal(t, []string{"main"}, heads(t, repo))
			hasSettings, err := git.New(repo.Dir).Check(ctx, "config", "--get", "branch.events.remote")
			require.NoError(t, err)
			require.False(t, hasSettings)
		})

		t.Run("deletes nothing when a branch moved after strata read it", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			tree := read(t, repo)
			repo.Switch("handler")
			repo.Commit("handler.go", "package orders\n\ntype Handler struct{}\n", "Name the handler")
			repo.Switch("main")

			err := deleter(repo).Delete(ctx, tree.Branches)

			require.ErrorContains(t, err, "strata deleted no branch")
			require.Equal(t, []string{"events", "handler", "main"}, heads(t, repo))
		})

		t.Run("refuses a branch that a worktree checked out after strata read it, and deletes nothing", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.SwitchNew("billing")
			repo.Commit("billing.go", "package billing\n", "Add billing")
			repo.Switch("main")
			tree := read(t, repo)
			repo.Worktree("events")

			err := deleter(repo).Delete(ctx, tree.Branches)

			require.ErrorContains(t, err, "events is checked out in ")
			require.Equal(t, []string{"billing", "events", "main"}, heads(t, repo))
		})
	})

	t.Run("trunk has", func(t *testing.T) {
		t.Run("the trunk has the changes of a squash-merged branch, and not of a branch with work that the trunk lacks", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.SwitchNew("billing")
			repo.Commit("billing.go", "package billing\n", "Add billing")
			repo.Switch("main")
			repo.SquashMergeOnOrigin("events")
			repo.Git("fetch", "-q", "origin")

			has, err := deleter(repo).TrunkHas(ctx, read(t, repo).Branches)

			require.NoError(t, err)
			require.Equal(t, map[string]bool{"billing": false, "events": true}, has)
		})
	})
}
