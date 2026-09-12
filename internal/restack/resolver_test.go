//go:build integration

package restack_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/restack"
	"github.com/stretchr/testify/require"
)

func TestResolver(t *testing.T) {
	ctx := context.Background()
	plan := func(t *testing.T, repo *gittest.Repo) restack.Plan {
		t.Helper()
		g := git.New(repo.Dir)
		p, err := restack.NewPlanner(g, g.Fetch, "origin/main", []string{"refs/heads/"}).Plan(ctx)
		require.NoError(t, err)
		return p
	}
	resolver := func(repo *gittest.Repo) *restack.Resolver {
		g := git.New(repo.Dir)
		return restack.NewResolver(g, restack.NewMover(g))
	}
	commit := func(t *testing.T, repo *gittest.Repo, rev string) string {
		t.Helper()
		return strings.TrimSpace(repo.Git("rev-parse", rev))
	}
	heads := func(t *testing.T, repo *gittest.Repo) string {
		t.Helper()
		return repo.Git("for-each-ref", "--format=%(refname) %(objectname)", "refs/heads/")
	}
	syncWorktree := func(t *testing.T, repo *gittest.Repo) string {
		t.Helper()
		dir, err := filepath.EvalSymlinks(filepath.Join(repo.Dir, ".git"))
		require.NoError(t, err)
		return filepath.Join(dir, "strata", "sync", "worktree")
	}
	conflicted := func(t *testing.T) *gittest.Repo {
		t.Helper()
		repo := gittest.New(t)
		repo.SwitchNew("events")
		repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
		repo.SwitchNew("handler")
		repo.Commit("handler.go", "package orders\n", "Add the handler")
		repo.Switch("main")
		repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
		return repo
	}
	resolve := func(t *testing.T, repo *gittest.Repo) {
		t.Helper()
		worktree := repo.In(syncWorktree(t, repo))
		worktree.Write("README.md", "shop\n\nopen all day on weekdays\n")
		worktree.Git("add", "README.md")
		_, err := git.New(worktree.Dir).Run(ctx, "-c", "core.editor=true", "rebase", "--continue")
		require.NoError(t, err)
	}

	t.Run("start", func(t *testing.T) {
		t.Run("stops at the conflict in the sync worktree and moves no branch", func(t *testing.T) {
			repo := conflicted(t)
			before := heads(t, repo)

			pending, err := resolver(repo).Start(ctx, plan(t, repo), "handler")

			require.NoError(t, err)
			require.Equal(t, restack.Pending{State: restack.RebaseWaits, Stack: "events", Worktree: syncWorktree(t, repo)}, pending)
			require.Equal(t, "UU README.md\n", repo.In(syncWorktree(t, repo)).Git("status", "--porcelain"))
			require.Equal(t, before, heads(t, repo))
		})

		t.Run("leaves the conflict to the user, with both sides in the file", func(t *testing.T) {
			repo := conflicted(t)

			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")

			require.NoError(t, err)
			content := repo.In(syncWorktree(t, repo)).Read("README.md")
			require.Contains(t, content, "<<<<<<<")
			require.Contains(t, content, "open all day")
			require.Contains(t, content, "open on weekdays")
		})

		t.Run("keeps a record of the branches of the stack and their old tips", func(t *testing.T) {
			repo := conflicted(t)
			eventsTip := commit(t, repo, "events")

			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")

			require.NoError(t, err)
			record, err := os.ReadFile(filepath.Join(repo.Dir, ".git", "strata", "sync", "plan"))
			require.NoError(t, err)
			require.Contains(t, string(record), "events")
			require.Contains(t, string(record), eventsTip)
		})

		t.Run("refuses a stack with a merge commit, because a sync rebase does not replay it", func(t *testing.T) {
			repo := conflicted(t)
			repo.Switch("handler")
			repo.Git("switch", "-q", "-c", "side", "events")
			repo.Commit("side.go", "package orders\n", "Add a side")
			repo.Switch("handler")
			repo.Git("merge", "-q", "--no-ff", "-m", "Merge the side", "side")
			repo.Git("branch", "-q", "-D", "side")
			repo.Switch("main")
			before := heads(t, repo)

			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")

			require.ErrorContains(t, err, "handler has a merge commit")
			require.Equal(t, before, heads(t, repo))
			require.NoFileExists(t, filepath.Join(repo.Dir, ".git", "strata", "sync", "plan"))
		})

		t.Run("refuses to start while another strata runs a sync rebase, and writes no record", func(t *testing.T) {
			repo := conflicted(t)
			p := plan(t, repo)
			holdSyncLock(t, repo)

			_, err := resolver(repo).Start(ctx, p, "events")

			require.ErrorContains(t, err, "another strata runs a sync rebase")
			require.NoFileExists(t, filepath.Join(repo.Dir, ".git", "strata", "sync", "plan"))
		})

		t.Run("refuses a stack with no conflict", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")

			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")

			require.ErrorContains(t, err, "the stack of events has no conflict")
		})

		t.Run("refuses to start a second sync rebase while one waits", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)

			_, err = resolver(repo).Start(ctx, plan(t, repo), "events")

			require.ErrorContains(t, err, "waits in "+syncWorktree(t, repo))
			pending, err := resolver(repo).Pending(ctx)
			require.NoError(t, err)
			require.Equal(t, restack.RebaseWaits, pending.State)
		})
	})

	t.Run("pending", func(t *testing.T) {
		t.Run("with no sync rebase there is nothing to finish", func(t *testing.T) {
			pending, err := resolver(conflicted(t)).Pending(ctx)

			require.NoError(t, err)
			require.Equal(t, restack.Pending{State: restack.NoRebase}, pending)
		})

		t.Run("a sync rebase that stopped at a conflict waits", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)

			pending, err := resolver(repo).Pending(ctx)

			require.NoError(t, err)
			require.Equal(t, restack.RebaseWaits, pending.State)
		})

		t.Run("a sync rebase that the user continued is done", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)
			resolve(t, repo)

			pending, err := resolver(repo).Pending(ctx)

			require.NoError(t, err)
			require.Equal(t, restack.RebaseDone, pending.State)
			require.Equal(t, "events", pending.Stack)
			require.Equal(t, commit(t, repo, "refs/strata/sync/events"), pending.Plan.Outcomes["events"].NewTip)
			require.Equal(t, commit(t, repo, "refs/strata/sync/handler"), pending.Plan.Outcomes["handler"].NewTip)
		})

		t.Run("a sync rebase that the user aborted is aborted", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)
			repo.In(syncWorktree(t, repo)).Git("rebase", "--abort")

			pending, err := resolver(repo).Pending(ctx)

			require.NoError(t, err)
			require.Equal(t, restack.RebaseAborted, pending.State)
		})
	})

	t.Run("finish", func(t *testing.T) {
		t.Run("moves the stack to the resolved commits, and removes the record, the sync worktree and the refs", func(t *testing.T) {
			repo := conflicted(t)
			worktreesBefore := repo.Git("worktree", "list", "--porcelain")
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)
			resolve(t, repo)

			result, err := resolver(repo).Finish(ctx)

			require.NoError(t, err)
			require.Equal(t, restack.Result{Moved: 1}, result)
			require.Equal(t, "shop\n\nopen all day on weekdays\n", repo.Git("show", "events:README.md"))
			require.Equal(t, commit(t, repo, "origin/main"), commit(t, repo, "events^"))
			require.Equal(t, commit(t, repo, "events"), commit(t, repo, "handler^"))
			require.NoFileExists(t, filepath.Join(repo.Dir, ".git", "strata", "sync", "plan"))
			require.Equal(t, worktreesBefore, repo.Git("worktree", "list", "--porcelain"))
			require.Empty(t, repo.Git("for-each-ref", "refs/strata/"))
		})

		t.Run("a branch that changed after the resolve makes the stack stay", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)
			resolve(t, repo)
			eventsBefore := commit(t, repo, "events")
			repo.Git("branch", "-f", "handler", "events")

			result, err := resolver(repo).Finish(ctx)

			require.NoError(t, err)
			require.Equal(t, 0, result.Moved)
			require.Len(t, result.Stayed, 1)
			require.Equal(t, eventsBefore, commit(t, repo, "events"))
		})

		t.Run("after an abort, removes the record, the sync worktree and every ref, and no branch moved", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("events.go", "package orders\n", "Name the events")
			repo.SwitchNew("handler")
			repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
			repo.Switch("main")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			before := heads(t, repo)
			worktreesBefore := repo.Git("worktree", "list", "--porcelain")
			_, err := resolver(repo).Start(ctx, plan(t, repo), "handler")
			require.NoError(t, err)
			require.NotEmpty(t, repo.Git("for-each-ref", "refs/strata/sync/events"))
			repo.In(syncWorktree(t, repo)).Git("rebase", "--abort")

			result, err := resolver(repo).Finish(ctx)

			require.NoError(t, err)
			require.Equal(t, restack.Result{}, result)
			require.Equal(t, before, heads(t, repo))
			require.NoFileExists(t, filepath.Join(repo.Dir, ".git", "strata", "sync", "plan"))
			require.Equal(t, worktreesBefore, repo.Git("worktree", "list", "--porcelain"))
			require.Empty(t, repo.Git("for-each-ref", "refs/strata/"))
		})

		t.Run("refuses while the sync rebase waits", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)

			_, err = resolver(repo).Finish(ctx)

			require.ErrorContains(t, err, "git rebase --continue")
		})

		t.Run("refuses while another strata runs a sync rebase, and moves no branch", func(t *testing.T) {
			repo := conflicted(t)
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)
			resolve(t, repo)
			before := heads(t, repo)
			holdSyncLock(t, repo)

			_, err = resolver(repo).Finish(ctx)

			require.ErrorContains(t, err, "another strata runs a sync rebase")
			require.Equal(t, before, heads(t, repo))
			require.FileExists(t, filepath.Join(repo.Dir, ".git", "strata", "sync", "plan"))
		})

		t.Run("with commit.gpgsign true, the resolved commits have signatures", func(t *testing.T) {
			repo := conflicted(t)
			repo.SignWithFakeGPG()
			_, err := resolver(repo).Start(ctx, plan(t, repo), "events")
			require.NoError(t, err)
			resolve(t, repo)

			_, err = resolver(repo).Finish(ctx)

			require.NoError(t, err)
			for _, rev := range []string{"events", "handler"} {
				require.Contains(t, repo.Git("cat-file", "commit", rev), "\ngpgsig ", "%s has no signature", rev)
			}
		})
	})
}

// holdSyncLock takes the lock of the sync rebase as a second strata does.
func holdSyncLock(t *testing.T, repo *gittest.Repo) {
	t.Helper()
	dir := filepath.Join(repo.Dir, ".git", "strata", "sync")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	t.Cleanup(func() { f.Close() })
}
