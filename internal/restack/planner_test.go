//go:build integration

package restack_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/stretchr/testify/require"
)

func TestPlanner(t *testing.T) {
	plan := func(t *testing.T, repo *gittest.Repo, trunk string) restack.Plan {
		t.Helper()
		g := git.New(repo.Dir)
		p, err := restack.NewPlanner(g, g.Fetch, trunk, []string{"refs/heads/"}).Plan(context.Background(), nil)
		require.NoError(t, err)
		return p
	}
	outcomes := func(p restack.Plan) []string {
		var out []string
		for _, b := range p.Tree.Branches {
			out = append(out, b.Name+": "+p.Outcomes[b.Name].Text())
		}
		return out
	}
	heads := func(t *testing.T, repo *gittest.Repo) string {
		t.Helper()
		return repo.Git("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
	}

	t.Run("plan", func(t *testing.T) {
		t.Run("a branch on the trunk moves onto the new trunk after the remote trunk gets a commit", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: moves onto origin/main"}, outcomes(p))
		})

		t.Run("a branch on the tip of its parent is up to date when nothing moves", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: up to date", "handler: up to date"}, outcomes(p))
		})

		t.Run("a child behind its parent moves onto its parent while the trunk has no new commit", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("events")
			repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: up to date", "handler: moves onto events"}, outcomes(p))
		})

		t.Run("the children of a branch that moves move too", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			p := plan(t, repo, "origin/main")

			require.Equal(t, []string{"events: moves onto origin/main", "handler: moves onto events"}, outcomes(p))
		})

		t.Run("the fetch brings the commits of the remote trunk into the trunk", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all night\n", "Open all night")

			p := plan(t, repo, "origin/main")

			require.Equal(t, 2, p.NewCommits)
			require.Equal(t, "Open all night", strings.TrimSpace(repo.Git("log", "-1", "--format=%s", "origin/main")))
		})

		t.Run("a local trunk fetches nothing", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			p := plan(t, repo, "main")

			require.Equal(t, 0, p.NewCommits)
			require.Equal(t, "Start the shop", strings.TrimSpace(repo.Git("log", "-1", "--format=%s", "origin/main")))
			require.Equal(t, []string{"events: up to date"}, outcomes(p))
		})

		t.Run("reports the fetch, the read, each branch it replays and the worktree check, in order", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			g := git.New(repo.Dir)
			var steps []string

			_, err := restack.NewPlanner(g, g.Fetch, "origin/main", []string{"refs/heads/"}).
				Plan(context.Background(), func(step string) { steps = append(steps, step) })

			require.NoError(t, err)
			require.Equal(t, []string{
				"fetching origin",
				"reading the branches",
				"replaying events (1 of 2)",
				"replaying handler (2 of 2)",
				"checking the worktrees",
			}, steps)
		})

		t.Run("moves no branch", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			before := heads(t, repo)

			p := plan(t, repo, "origin/main")
			require.Equal(t, "moves onto origin/main", p.Outcomes["events"].Text())

			require.Equal(t, before, heads(t, repo))
		})

		t.Run("merged branches", func(t *testing.T) {
			t.Run("a branch that the remote squash merged is merged, and strata deletes it", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Commit("placed.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
				repo.Switch("main")
				repo.SquashMergeOnOrigin("events")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: merged: strata deletes it"}, outcomes(p))
			})

			t.Run("the children of a merged branch move onto the trunk with only their own commits", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("main")
				repo.SquashMergeOnOrigin("events")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: merged: strata deletes it", "handler: moves onto origin/main"}, outcomes(p))
				require.Equal(t, 1, p.Tree.Branches[p.Tree.Index("handler")].Commits)
			})

			t.Run("a merged branch that a worktree has checked out stays", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("main")
				worktree, err := filepath.EvalSymlinks(repo.Worktree("events").Dir)
				require.NoError(t, err)
				repo.SquashMergeOnOrigin("events")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: merged, checked out in " + worktree, "handler: moves onto origin/main"}, outcomes(p))
			})

			t.Run("a child that the remote merged too is merged, and its children go onto the trunk", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.SwitchNew("api")
				repo.Commit("api.go", "package api\n", "Expose the handler")
				repo.Switch("main")
				repo.SquashMergeOnOrigin("events")
				repo.SquashMergeOnOrigin("handler")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{
					"events: merged: strata deletes it",
					"handler: merged: strata deletes it",
					"api: moves onto origin/main",
				}, outcomes(p))
			})

			t.Run("a branch whose changes the trunk has only in part is not merged", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Commit("placed.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
				repo.Switch("main")
				repo.CommitOnOrigin("events.go", "package orders\n", "Pick the events file into main")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: moves onto origin/main"}, outcomes(p))
			})

			t.Run("a branch whose commits change nothing in total is not merged", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Git("rm", "-q", "events.go")
				repo.Git("commit", "-q", "-m", "Remove the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: moves onto origin/main"}, outcomes(p))
			})

			t.Run("a branch whose remote branch is gone but whose changes the trunk does not have moves, with a note", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Git("push", "-q", "-u", "origin", "events")
				repo.Git("push", "-q", "origin", "--delete", "events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: moves onto origin/main; remote branch gone, not merged"}, outcomes(p))
			})
		})

		t.Run("replay", func(t *testing.T) {
			subjects := func(t *testing.T, repo *gittest.Repo, from, to string) []string {
				t.Helper()
				return strings.Split(strings.TrimSpace(repo.Git("log", "--format=%s", from+".."+to)), "\n")
			}
			commit := func(t *testing.T, repo *gittest.Repo, rev string) string {
				t.Helper()
				return strings.TrimSpace(repo.Git("rev-parse", rev))
			}

			t.Run("a branch that moves gets a new tip with its own commits on the new trunk", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				newTip := p.Outcomes["events"].NewTip
				require.Equal(t, []string{"Name the events"}, subjects(t, repo, "origin/main", newTip))
				require.Equal(t, commit(t, repo, "origin/main"), commit(t, repo, newTip+"^"))
			})

			t.Run("a child behind its parent gets a new tip on the tip of its parent", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("events")
				repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")

				p := plan(t, repo, "origin/main")

				newTip := p.Outcomes["handler"].NewTip
				require.Equal(t, []string{"Add the handler"}, subjects(t, repo, "events", newTip))
				require.Equal(t, commit(t, repo, "events"), commit(t, repo, newTip+"^"))
			})

			t.Run("the child of a merged branch gets a new tip with only its own commits", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.SquashMergeOnOrigin("events")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"Add the handler"}, subjects(t, repo, "origin/main", p.Outcomes["handler"].NewTip))
			})

			t.Run("a branch with no own commits gets the new tip of its parent", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Git("branch", "empty", "events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: moves onto origin/main", "empty: moves onto events"}, outcomes(p))
				require.Equal(t, p.Outcomes["events"].NewTip, p.Outcomes["empty"].NewTip)
			})

			t.Run("a branch whose replay stops stays, and the plan names the files of the conflict", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: conflict in README.md"}, outcomes(p))
				require.Equal(t, 1, p.StacksWithConflict())
			})

			t.Run("a branch that merged the trunk into itself stays", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.Git("fetch", "-q")
				repo.Git("merge", "-q", "--no-edit", "origin/main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all night\n", "Open all night")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: merge commit"}, outcomes(p))
				require.Equal(t, 0, p.StacksWithConflict())
			})

			t.Run("when a branch stays, the branches of its stack that would change stay too, and other stacks move", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("billing")
				repo.Commit("billing.go", "package billing\n", "Add billing")
				repo.Switch("main")
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.SwitchNew("api")
				repo.Commit("api.go", "package api\n", "Expose the handler")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{
					"billing: moves onto origin/main",
					"events: stays: handler cannot move",
					"handler: stays: conflict in README.md",
					"api: stays: handler cannot move",
				}, outcomes(p))
			})

			t.Run("a merged branch of a stack that stays is not deleted", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.SquashMergeOnOrigin("events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: handler cannot move", "handler: stays: conflict in README.md"}, outcomes(p))
			})

			t.Run("a plan with a conflict moves no branch and changes no file", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				before := heads(t, repo)

				p := plan(t, repo, "origin/main")
				require.Equal(t, []string{"events: stays: conflict in README.md"}, outcomes(p))

				require.Equal(t, before, heads(t, repo))
				require.Empty(t, repo.Git("status", "--porcelain"))
			})
		})

		t.Run("worktrees", func(t *testing.T) {
			realPath := func(t *testing.T, dir string) string {
				t.Helper()
				path, err := filepath.EvalSymlinks(dir)
				require.NoError(t, err)
				return path
			}

			t.Run("a branch that a rebase in another worktree uses stays, and its stack stays too", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.Switch("main")
				worktree := repo.Worktree("handler")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				worktree.Git("fetch", "-q")
				worktree.StartRebase("origin/main")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{
					"events: stays: handler cannot move",
					"handler: stays: a rebase in " + realPath(t, worktree.Dir) + " uses it",
				}, outcomes(p))
			})

			t.Run("a branch that a rebase in the main worktree uses stays", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.Git("fetch", "-q")
				repo.StartRebase("origin/main")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: a rebase in " + realPath(t, repo.Dir) + " uses it"}, outcomes(p))
			})

			t.Run("a branch whose worktree has changes in a file that the move changes stays", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := repo.Worktree("events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				worktree.Write("README.md", "shop\n\nmine\n")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: changes in " + realPath(t, worktree.Dir) + ": README.md"}, outcomes(p))
			})

			t.Run("a staged change in a file that the move changes makes the branch stay", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := repo.Worktree("events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				worktree.Write("README.md", "shop\n\nmine\n")
				worktree.Git("add", "README.md")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: changes in " + realPath(t, worktree.Dir) + ": README.md"}, outcomes(p))
			})

			t.Run("an untracked file where the move adds a file makes the branch stay", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := repo.Worktree("events")
				repo.CommitOnOrigin("hours/weekdays.txt", "9 to 5\n", "Add the hours")
				worktree.Write("hours/weekdays.txt", "mine\n")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: stays: changes in " + realPath(t, worktree.Dir) + ": hours/"}, outcomes(p))
			})

			t.Run("a branch still moves when its worktree has changes only in files that the move does not change", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := repo.Worktree("events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				worktree.Write("events.go", "package orders\n\n// mine\n")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: moves onto origin/main, checked out in " + realPath(t, worktree.Dir)}, outcomes(p))
			})

			t.Run("the plan names the worktree that strata runs in", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{"events: moves onto origin/main, checked out in " + realPath(t, repo.Dir)}, outcomes(p))
			})

			t.Run("a branch whose worktree folder is gone stays, and the plan of the other stacks goes on", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("billing")
				repo.Commit("billing.go", "package billing\n", "Add billing")
				repo.Switch("main")
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := realPath(t, repo.Worktree("events").Dir)
				require.NoError(t, os.RemoveAll(worktree))
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

				p := plan(t, repo, "origin/main")

				require.Equal(t, []string{
					"billing: moves onto origin/main",
					"events: stays: its worktree " + worktree + " is not there; run git worktree prune if you deleted it",
				}, outcomes(p))
			})
		})
	})

	t.Run("replan", func(t *testing.T) {
		t.Run("after a move, plans the stacks that are left against the trunk of the first plan, and fetches nothing", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("billing")
			repo.Commit("billing.go", "package billing\n", "Add billing")
			repo.Switch("main")
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			g := git.New(repo.Dir)
			planner := restack.NewPlanner(g, g.Fetch, "origin/main", []string{"refs/heads/"})
			first, err := planner.Plan(context.Background(), nil)
			require.NoError(t, err)
			_, err = restack.NewMover(g).Move(context.Background(), first.ForStackOf("billing"))
			require.NoError(t, err)
			repo.CommitOnOrigin("README.md", "shop\n\nopen all night\n", "Open all night")

			again, err := planner.Replan(context.Background(), nil)

			require.NoError(t, err)
			require.Equal(t, first.TrunkTip, again.TrunkTip)
			require.Equal(t, []string{"billing: up to date", "events: moves onto origin/main"}, outcomes(again))
		})
	})
}
