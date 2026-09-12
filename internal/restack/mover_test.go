//go:build integration

package restack_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/stretchr/testify/require"
)

func TestMover(t *testing.T) {
	plan := func(t *testing.T, repo *gittest.Repo) restack.Plan {
		t.Helper()
		g := git.New(repo.Dir)
		p, err := restack.NewPlanner(g, g.Fetch, "origin/main", []string{"refs/heads/"}).Plan(context.Background())
		require.NoError(t, err)
		return p
	}
	move := func(t *testing.T, repo *gittest.Repo, p restack.Plan) restack.Result {
		t.Helper()
		result, err := restack.NewMover(git.New(repo.Dir)).Move(context.Background(), p)
		require.NoError(t, err)
		return result
	}
	commit := func(t *testing.T, repo *gittest.Repo, rev string) string {
		t.Helper()
		return strings.TrimSpace(repo.Git("rev-parse", rev))
	}
	read := func(t *testing.T, repo *gittest.Repo) stack.Tree {
		t.Helper()
		tree, err := stack.NewReader(git.New(repo.Dir), "origin/main", []string{"refs/heads/"}).Read(context.Background())
		require.NoError(t, err)
		return tree
	}

	t.Run("move", func(t *testing.T) {
		t.Run("moves each branch of a stack to its new tip, and no branch is then behind its parent", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("events")
			repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			p := plan(t, repo)

			result := move(t, repo, p)

			require.Equal(t, restack.Result{Moved: 1}, result)
			require.Equal(t, p.Outcomes["events"].NewTip, commit(t, repo, "events"))
			require.Equal(t, p.Outcomes["handler"].NewTip, commit(t, repo, "handler"))
			tree := read(t, repo)
			require.Equal(t, []stack.Branch{
				{Name: "events", Parent: "origin/main", Behind: 0, Commits: 2},
				{Name: "handler", Parent: "events", Behind: 0, Commits: 1, Level: 1},
			}, placements(tree))
		})

		t.Run("deletes a merged branch and its settings in the transaction of its stack, and its children sit on the trunk", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Git("push", "-q", "-u", "origin", "events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			repo.SquashMergeOnOrigin("events")
			p := plan(t, repo)

			move(t, repo, p)

			exists, err := git.New(repo.Dir).Check(context.Background(), "rev-parse", "--verify", "--quiet", "refs/heads/events")
			require.NoError(t, err)
			require.False(t, exists)
			hasSettings, err := git.New(repo.Dir).Check(context.Background(), "config", "--get", "branch.events.remote")
			require.NoError(t, err)
			require.False(t, hasSettings)
			require.Equal(t, []stack.Branch{{Name: "handler", Parent: "origin/main", Commits: 1}}, placements(read(t, repo)))
		})

		t.Run("deletes a merged branch that has no children, and counts its stack as moved", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.SquashMergeOnOrigin("events")
			p := plan(t, repo)

			result := move(t, repo, p)

			require.Equal(t, restack.Result{Moved: 1}, result)
			exists, err := git.New(repo.Dir).Check(context.Background(), "rev-parse", "--verify", "--quiet", "refs/heads/events")
			require.NoError(t, err)
			require.False(t, exists)
		})

		t.Run("keeps a merged branch when the plan keeps merged branches", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			repo.SquashMergeOnOrigin("events")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo).KeepMerged()

			move(t, repo, p)

			require.Equal(t, "merged: strata keeps it", p.Outcomes["events"].Text())
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
			require.Equal(t, p.Outcomes["handler"].NewTip, commit(t, repo, "handler"))
		})

		t.Run("keeps a merged branch that a worktree has checked out, and moves its children", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			repo.Worktree("events")
			repo.SquashMergeOnOrigin("events")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo)

			result := move(t, repo, p)

			require.Equal(t, restack.Result{Moved: 1}, result)
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
			require.Equal(t, p.Outcomes["handler"].NewTip, commit(t, repo, "handler"))
		})

		t.Run("a branch that changed after the plan keeps its whole stack where it is", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo)
			repo.Git("branch", "-f", "handler", "events")

			result := move(t, repo, p)

			require.Equal(t, 0, result.Moved)
			require.Len(t, result.Stayed, 1)
			require.Equal(t, "events", result.Stayed[0].Stack)
			require.Contains(t, result.Stayed[0].Reason, "refs/heads/handler")
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
			require.Equal(t, eventsBefore, commit(t, repo, "handler"))
		})

		t.Run("a rebase that starts on a branch after the plan keeps its stack where it is", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
			repo.Switch("main")
			repo.SwitchNew("weekends")
			repo.Commit("README.md", "shop\n\nopen on weekends\n", "Open on weekends")
			repo.Switch("main")
			repo.CommitOnOrigin("hours.txt", "9 to 5\n", "Add the hours")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo)
			repo.Worktree("events").StartRebase("weekends")

			result := move(t, repo, p)

			require.Equal(t, 1, result.Moved)
			require.Len(t, result.Stayed, 1)
			require.Equal(t, "events", result.Stayed[0].Stack)
			require.Contains(t, result.Stayed[0].Reason, "a rebase in")
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
			require.Equal(t, p.Outcomes["weekends"].NewTip, commit(t, repo, "weekends"))
		})

		t.Run("a stack with a conflict stays, and the other stacks move", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("billing")
			repo.Commit("billing.go", "package billing\n", "Add billing")
			repo.Switch("main")
			repo.SwitchNew("events")
			repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo)

			result := move(t, repo, p)

			require.Equal(t, restack.Result{Moved: 1}, result)
			require.Equal(t, p.Outcomes["billing"].NewTip, commit(t, repo, "billing"))
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
		})

		t.Run("a stack with a checked-out branch that must move stays", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo)

			result := move(t, repo, p)

			require.Equal(t, 0, result.Moved)
			require.Len(t, result.Stayed, 1)
			require.Contains(t, result.Stayed[0].Reason, "checked out in")
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
		})

		t.Run("moves no branch when commit.gpgsign is true", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			repo.Git("config", "commit.gpgsign", "true")
			eventsBefore := commit(t, repo, "events")
			p := plan(t, repo)

			_, err := restack.NewMover(git.New(repo.Dir)).Move(context.Background(), p)

			require.ErrorContains(t, err, "commit.gpgsign")
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
		})
	})
}

func placements(tree stack.Tree) []stack.Branch {
	var out []stack.Branch
	for _, b := range tree.Branches {
		out = append(out, stack.Branch{Name: b.Name, Parent: b.Parent, Level: b.Level, Commits: b.Commits, Behind: b.Behind})
	}
	return out
}
