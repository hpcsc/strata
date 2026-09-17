//go:build integration

package refs_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/refs"
	"github.com/stretchr/testify/require"
)

func TestWatcher(t *testing.T) {
	requireCheck := func(t *testing.T, checks <-chan struct{}, within time.Duration) {
		t.Helper()
		select {
		case <-checks:
		case <-time.After(within):
			require.Fail(t, "the watcher called no check in "+within.String())
		}
	}
	requireNoCheck := func(t *testing.T, checks <-chan struct{}, during time.Duration) {
		t.Helper()
		select {
		case <-checks:
			require.Fail(t, "the watcher called check")
		case <-time.After(during):
		}
	}
	run := func(t *testing.T, repo *gittest.Repo) <-chan struct{} {
		t.Helper()
		checks := make(chan struct{}, 100)
		ctx, cancel := context.WithCancel(context.Background())
		stopped := make(chan struct{})
		go func() {
			defer close(stopped)
			refs.NewWatcher(git.New(repo.Dir)).Run(ctx, func() { checks <- struct{}{} })
		}()
		t.Cleanup(func() {
			cancel()
			<-stopped
		})
		return checks
	}
	ready := func(t *testing.T, repo *gittest.Repo) <-chan struct{} {
		t.Helper()
		checks := run(t, repo)
		requireCheck(t, checks, 3*time.Second)
		return checks
	}
	withBranch := func(t *testing.T) *gittest.Repo {
		t.Helper()
		repo := gittest.New(t)
		repo.SwitchNew("events")
		repo.Commit("events.go", "package orders\n", "Name the events")
		return repo
	}

	t.Run("watch", func(t *testing.T) {
		t.Run("calls check once the watches are ready", func(t *testing.T) {
			checks := run(t, withBranch(t))

			requireCheck(t, checks, 3*time.Second)
		})

		t.Run("calls check after a commit on a branch", func(t *testing.T) {
			repo := withBranch(t)
			checks := ready(t, repo)

			repo.Commit("events.go", "package orders\n\ntype Placed struct{}\n", "Add Placed")

			requireCheck(t, checks, 2*time.Second)
		})

		t.Run("calls check after a commit on a branch in a folder that git made after the watch started", func(t *testing.T) {
			repo := withBranch(t)
			checks := ready(t, repo)
			repo.SwitchNew("team/billing")
			requireCheck(t, checks, 2*time.Second)

			repo.Commit("billing.go", "package billing\n", "Add billing")

			requireCheck(t, checks, 2*time.Second)
		})

		t.Run("calls check after a fetch into a folder that git pack-refs removed", func(t *testing.T) {
			repo := withBranch(t)
			repo.Git("remote", "set-head", "origin", "-d")
			checks := ready(t, repo)
			repo.Git("pack-refs", "--all")
			require.NoDirExists(t, filepath.Join(repo.Dir, ".git", "refs", "remotes", "origin"))
			requireCheck(t, checks, 2*time.Second)
			repo.CommitOnOrigin("README.md", "shop\n\nopen\n", "Open the shop")
			repo.Git("fetch", "-q")
			requireCheck(t, checks, 2*time.Second)

			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			repo.Git("fetch", "-q")

			requireCheck(t, checks, 2*time.Second)
		})

		t.Run("calls check after another worktree leaves its branch", func(t *testing.T) {
			repo := withBranch(t)
			repo.Switch("main")
			worktree := repo.Worktree("events")
			checks := ready(t, repo)

			worktree.Git("switch", "-q", "--detach")

			requireCheck(t, checks, 2*time.Second)
		})

		t.Run("calls check once for ref changes that come close together", func(t *testing.T) {
			repo := withBranch(t)
			checks := ready(t, repo)

			for i := range 5 {
				repo.Git("branch", fmt.Sprintf("billing-%d", i))
			}

			requireCheck(t, checks, 2*time.Second)
			requireNoCheck(t, checks, time.Second)
		})

		t.Run("does not call check after git status", func(t *testing.T) {
			repo := withBranch(t)
			checks := ready(t, repo)
			repo.Write("handler.go", "package orders\n")

			repo.Git("status")

			requireNoCheck(t, checks, time.Second)
		})
	})

	t.Run("poll", func(t *testing.T) {
		t.Run("calls check each 2 s when a folder under refs cannot be read", func(t *testing.T) {
			if os.Geteuid() == 0 {
				t.Skip("root can read a folder with no permissions")
			}
			repo := withBranch(t)
			repo.Git("branch", "team/billing")
			locked := filepath.Join(repo.Dir, ".git", "refs", "heads", "team")
			require.NoError(t, os.Chmod(locked, 0))
			t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

			checks := run(t, repo)

			requireNoCheck(t, checks, time.Second)
			requireCheck(t, checks, 2*time.Second)
			requireCheck(t, checks, 3*time.Second)
		})
	})
}
