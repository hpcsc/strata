//go:build integration

package restack_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

		t.Run("a branch that changed after the plan makes its whole stack stay", func(t *testing.T) {
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

		t.Run("a rebase that starts on a branch after the plan makes its stack stay", func(t *testing.T) {
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

		t.Run("signed commits", func(t *testing.T) {
			signed := func(t *testing.T, repo *gittest.Repo, from, to string) []string {
				t.Helper()
				var subjects []string
				for _, c := range strings.Fields(repo.Git("rev-list", "--reverse", from+".."+to)) {
					raw := repo.Git("cat-file", "commit", c)
					require.Contains(t, raw, "\ngpgsig ", "commit %s has no signature", c)
					subjects = append(subjects, strings.TrimSpace(repo.Git("log", "-1", "--format=%s", c)))
				}
				return subjects
			}
			tree := func(t *testing.T, repo *gittest.Repo, rev string) string {
				t.Helper()
				return commit(t, repo, rev+"^{tree}")
			}

			t.Run("signs each commit that a branch gets, with the tree and the message of the plan", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Commit("placed.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, []string{"Name the events", "Add Placed"}, signed(t, repo, "origin/main", "events"))
				require.Equal(t, tree(t, repo, p.Outcomes["events"].NewTip), tree(t, repo, "events"))
			})

			t.Run("a tree of branches moves in one sync rebase, and a child behind its parent moves onto the new tip of the parent", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("events")
				repo.SwitchNew("other")
				repo.Commit("other.go", "package orders\n", "Add the other")
				repo.Switch("events")
				repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				move(t, repo, p)

				require.Equal(t, []string{"Add the handler"}, signed(t, repo, "events", "handler"))
				require.Equal(t, []string{"Add the other"}, signed(t, repo, "events", "other"))
				require.Equal(t, commit(t, repo, "events"), commit(t, repo, "handler^"))
				require.Equal(t, commit(t, repo, "events"), commit(t, repo, "other^"))
				require.Equal(t, []string{"Name the events", "Add Placed"}, signed(t, repo, "origin/main", "events"))
			})

			t.Run("a child behind a parent that is up to date moves onto that parent, and the parent stays", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("events")
				repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
				repo.Switch("main")
				repo.SignWithFakeGPG()
				eventsBefore := commit(t, repo, "events")
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, eventsBefore, commit(t, repo, "events"))
				require.Equal(t, eventsBefore, commit(t, repo, "handler^"))
				require.Equal(t, []string{"Add the handler"}, signed(t, repo, "events", "handler"))
			})

			t.Run("the children of a merged branch move onto the new trunk with only their own commits", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("main")
				repo.SquashMergeOnOrigin("events")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				move(t, repo, p)

				require.Equal(t, []string{"Add the handler"}, signed(t, repo, "origin/main", "handler"))
			})

			t.Run("a commit whose change the new trunk has stays as an empty commit", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Commit("placed.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")
				repo.Switch("main")
				repo.CommitOnOrigin("events.go", "package orders\n", "Pick the events file into main")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				move(t, repo, p)

				require.Equal(t, []string{"Name the events", "Add Placed"}, signed(t, repo, "origin/main", "events"))
				require.Empty(t, repo.Git("diff", "events~2", "events~1"))
			})

			t.Run("moves a branch that a worktree has checked out to its signed tip", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				move(t, repo, p)

				require.Equal(t, []string{"Name the events"}, signed(t, repo, "origin/main", "HEAD"))
				require.Empty(t, repo.Git("status", "--porcelain"))
			})

			t.Run("moves a branch whose name holds characters that a shell reads", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("fix(ui);x")
				repo.Commit("ui.go", "package ui\n", "Fix the UI")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, []string{"Fix the UI"}, signed(t, repo, "origin/main", "fix(ui);x"))
			})

			t.Run("uses the todo list of strata when the user sets GIT_SEQUENCE_EDITOR", func(t *testing.T) {
				t.Setenv("GIT_SEQUENCE_EDITOR", "true")
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, []string{"Name the events"}, signed(t, repo, "origin/main", "events"))
			})

			t.Run("moves nothing while another strata runs a sync rebase", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				eventsBefore := commit(t, repo, "events")
				p := plan(t, repo)
				holdSyncLock(t, repo)

				_, err := restack.NewMover(git.New(repo.Dir)).Move(context.Background(), p)

				require.ErrorContains(t, err, "another strata runs a sync rebase")
				require.Equal(t, eventsBefore, commit(t, repo, "events"))
			})

			t.Run("an interrupt ends the sync rebase, and leaves no sync worktree and no ref", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				signing := filepath.Join(t.TempDir(), "signing")
				slowGPG := filepath.Join(t.TempDir(), "slow-gpg")
				require.NoError(t, os.WriteFile(slowGPG, []byte("#!/bin/sh\ntouch "+signing+"\nsleep 10\n"), 0o755))
				repo.Git("config", "gpg.program", slowGPG)
				eventsBefore := commit(t, repo, "events")
				worktreesBefore := repo.Git("worktree", "list", "--porcelain")
				p := plan(t, repo)
				ctx, interrupt := context.WithCancel(context.Background())
				t.Cleanup(interrupt)
				go func() {
					for ctx.Err() == nil {
						if _, err := os.Stat(signing); err == nil {
							interrupt()
						}
						time.Sleep(10 * time.Millisecond)
					}
				}()

				_, err := restack.NewMover(git.New(repo.Dir)).Move(ctx, p)

				require.Error(t, err)
				require.Equal(t, eventsBefore, commit(t, repo, "events"))
				require.Equal(t, worktreesBefore, repo.Git("worktree", "list", "--porcelain"))
				require.Empty(t, repo.Git("for-each-ref", "refs/strata/"))
			})

			t.Run("moves nothing while a sync rebase waits, and keeps that rebase", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("billing")
				repo.Commit("billing.go", "package billing\n", "Add billing")
				repo.Switch("main")
				repo.SwitchNew("events")
				repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				g := git.New(repo.Dir)
				waiting, err := restack.NewResolver(g, restack.NewMover(g)).Start(context.Background(), plan(t, repo), "events")
				require.NoError(t, err)
				billingBefore := commit(t, repo, "billing")

				_, err = restack.NewMover(g).Move(context.Background(), plan(t, repo))

				require.ErrorContains(t, err, "git rebase --continue")
				require.Equal(t, billingBefore, commit(t, repo, "billing"))
				require.Equal(t, "UU README.md\n", repo.In(waiting.Worktree).Git("status", "--porcelain"))
			})

			t.Run("leaves no sync worktree and no ref in refs/strata/sync/", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				worktreesBefore := repo.Git("worktree", "list", "--porcelain")
				p := plan(t, repo)

				move(t, repo, p)

				require.Equal(t, worktreesBefore, repo.Git("worktree", "list", "--porcelain"))
				require.Empty(t, repo.Git("for-each-ref", "refs/strata/"))
			})

			t.Run("a sync worktree that an earlier sync left behind does not stop the sync", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				repo.Git("worktree", "add", "-q", "--detach", filepath.Join(repo.Dir, ".git", "strata", "sync", "worktree"), "main")
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, []string{"Name the events"}, signed(t, repo, "origin/main", "events"))
			})

			t.Run("hooks of the repository do not run in the sync worktree", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				repo.SignWithFakeGPG()
				hooks, marker := t.TempDir(), filepath.Join(t.TempDir(), "hook-ran")
				require.NoError(t, os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755))
				repo.Git("config", "core.hooksPath", hooks)
				_, err := git.New(repo.Dir).Run(context.Background(), "worktree", "add", "-q", "--detach", filepath.Join(t.TempDir(), "probe"), "main")
				require.NoError(t, err)
				require.FileExists(t, marker, "the hook does not run for a checkout of the user")
				require.NoError(t, os.Remove(marker))
				p := plan(t, repo)

				move(t, repo, p)

				require.NoFileExists(t, marker)
			})
		})

		t.Run("in worktrees", func(t *testing.T) {
			t.Run("moves a branch that a linked worktree has checked out, with its files", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("main")
				worktree := repo.Worktree("handler")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, p.Outcomes["events"].NewTip, commit(t, repo, "events"))
				require.Equal(t, p.Outcomes["handler"].NewTip, commit(t, worktree, "HEAD"))
				require.Empty(t, worktree.Git("status", "--porcelain"))
				require.Equal(t, "shop\n\nopen all day\n", worktree.Git("show", "HEAD:README.md"))
			})

			t.Run("moves a branch that the worktree of strata has checked out", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				p := plan(t, repo)

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, p.Outcomes["events"].NewTip, commit(t, repo, "HEAD"))
				require.Equal(t, "events", strings.TrimSpace(repo.Git("branch", "--show-current")))
				require.Empty(t, repo.Git("status", "--porcelain"))
			})

			t.Run("moves a branch that a worktree checked out after the plan, with its files", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				p := plan(t, repo)
				worktree := repo.Worktree("events")

				result := move(t, repo, p)

				require.Equal(t, restack.Result{Moved: 1}, result)
				require.Equal(t, p.Outcomes["events"].NewTip, commit(t, worktree, "HEAD"))
				require.Empty(t, worktree.Git("status", "--porcelain"))
			})

			t.Run("keeps a merged branch that a worktree checked out after the plan, and moves its children", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("main")
				repo.SquashMergeOnOrigin("events")
				eventsBefore := commit(t, repo, "events")
				p := plan(t, repo)
				repo.Worktree("events")

				move(t, repo, p)

				require.Equal(t, eventsBefore, commit(t, repo, "events"))
				require.Equal(t, p.Outcomes["handler"].NewTip, commit(t, repo, "handler"))
			})

			t.Run("keeps uncommitted changes in files that the move does not change", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := repo.Worktree("events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				worktree.Write("events.go", "package orders\n\n// mine\n")
				p := plan(t, repo)

				move(t, repo, p)

				require.Equal(t, p.Outcomes["events"].NewTip, commit(t, worktree, "HEAD"))
				require.Equal(t, " M events.go\n", worktree.Git("status", "--porcelain"))
			})

			t.Run("a commit in a worktree after the plan makes the stack stay", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.SwitchNew("handler")
				repo.Commit("handler.go", "package orders\n", "Add the handler")
				repo.Switch("main")
				worktree := repo.Worktree("handler")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				eventsBefore := commit(t, repo, "events")
				p := plan(t, repo)
				worktree.Commit("handler.go", "package orders\n\n// more\n", "Continue the handler")
				handlerAfterCommit := commit(t, worktree, "HEAD")

				result := move(t, repo, p)

				require.Equal(t, 0, result.Moved)
				require.Len(t, result.Stayed, 1)
				require.Contains(t, result.Stayed[0].Reason, "it changed in")
				require.Equal(t, eventsBefore, commit(t, repo, "events"))
				require.Equal(t, handlerAfterCommit, commit(t, repo, "handler"))
			})

			t.Run("changes after the plan in a file that the move changes make the stack stay", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("events")
				repo.Commit("events.go", "package orders\n", "Name the events")
				repo.Switch("main")
				worktree := repo.Worktree("events")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				eventsBefore := commit(t, repo, "events")
				p := plan(t, repo)
				worktree.Write("README.md", "shop\n\nmine\n")

				result := move(t, repo, p)

				require.Equal(t, 0, result.Moved)
				require.Len(t, result.Stayed, 1)
				require.Contains(t, result.Stayed[0].Reason, "changes in")
				require.Equal(t, eventsBefore, commit(t, repo, "events"))
				require.Equal(t, "shop\n\nmine\n", worktree.Read("README.md"))
			})

			t.Run("a move that fails in a worktree keeps the new tip in a ref, and the reason gives a shell command that finishes it", func(t *testing.T) {
				repo := gittest.New(t)
				repo.SwitchNew("fix(ui);x")
				repo.Commit("ui.go", "package ui\n", "Fix the UI")
				repo.Switch("main")
				worktree := repo.Worktree("fix(ui);x")
				repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
				before := commit(t, repo, "fix(ui);x")
				p := plan(t, repo)
				newTip := p.Outcomes["fix(ui);x"].NewTip
				lock := filepath.Join(strings.TrimSpace(worktree.Git("rev-parse", "--absolute-git-dir")), "index.lock")
				require.NoError(t, os.WriteFile(lock, nil, 0o644))

				result := move(t, repo, p)

				require.Equal(t, 0, result.Moved)
				require.Len(t, result.Stayed, 1)
				require.Equal(t, newTip, commit(t, repo, "refs/strata/sync/fix(ui);x"))
				require.Equal(t, before, commit(t, repo, "fix(ui);x"))
				_, finish, found := strings.Cut(result.Stayed[0].Reason, "To finish the move, run: ")
				require.True(t, found, result.Stayed[0].Reason)

				require.NoError(t, os.Remove(lock))
				out, err := exec.Command("sh", "-c", finish).CombinedOutput()
				require.NoError(t, err, "%s: %s", finish, out)
				require.Equal(t, newTip, commit(t, repo, "fix(ui);x"))
			})
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
