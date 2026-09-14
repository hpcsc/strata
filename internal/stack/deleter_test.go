//go:build integration

package stack_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hpcsc/strata/internal/git"
	"github.com/hpcsc/strata/internal/gittest"
	"github.com/hpcsc/strata/internal/stack"
	"github.com/hpcsc/strata/internal/tmux"
	"github.com/stretchr/testify/require"
)

type memoryTmux struct {
	panes    []tmux.Pane
	panesErr error
	closeErr error
	closed   []string
}

func (m *memoryTmux) Panes(context.Context) ([]tmux.Pane, error) {
	if m.panesErr != nil {
		return nil, m.panesErr
	}
	return m.panes, nil
}

func (m *memoryTmux) ClosePane(_ context.Context, id string) error {
	if m.closeErr != nil {
		return m.closeErr
	}
	m.closed = append(m.closed, id)
	return nil
}

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
	deleterWith := func(repo *gittest.Repo, server *memoryTmux) *stack.Deleter {
		return stack.NewDeleter(git.New(repo.Dir), "origin/main", server)
	}
	deleter := func(repo *gittest.Repo) *stack.Deleter {
		return deleterWith(repo, &memoryTmux{})
	}
	checkWith := func(t *testing.T, repo *gittest.Repo, server *memoryTmux) []stack.Deletion {
		t.Helper()
		deletions, err := deleterWith(repo, server).Check(ctx, read(t, repo).Branches)
		require.NoError(t, err)
		return deletions
	}
	check := func(t *testing.T, repo *gittest.Repo) []stack.Deletion {
		t.Helper()
		return checkWith(t, repo, &memoryTmux{})
	}
	realPath := func(t *testing.T, dir string) string {
		t.Helper()
		path, err := filepath.EvalSymlinks(dir)
		require.NoError(t, err)
		return path
	}
	withEvents := func(t *testing.T) *gittest.Repo {
		t.Helper()
		repo := gittest.New(t)
		repo.SwitchNew("events")
		repo.Commit("events.go", "package orders\n", "Name the events")
		repo.Switch("main")
		return repo
	}

	t.Run("check", func(t *testing.T) {
		t.Run("the trunk has the changes of a squash-merged branch, and not of a branch with work that the trunk lacks", func(t *testing.T) {
			repo := withEvents(t)
			repo.SwitchNew("billing")
			repo.Commit("billing.go", "package billing\n", "Add billing")
			repo.Switch("main")
			repo.SquashMergeOnOrigin("events")
			repo.Git("fetch", "-q", "origin")

			deletions := check(t, repo)

			trunkHas := map[string]bool{}
			for _, del := range deletions {
				trunkHas[del.Branch.Name] = del.TrunkHas
			}
			require.Equal(t, map[string]bool{"billing": false, "events": true}, trunkHas)
		})

		t.Run("the delete of a branch checked out in another worktree removes that worktree", func(t *testing.T) {
			repo := withEvents(t)
			worktree := repo.Worktree("events")

			deletions := check(t, repo)

			require.Equal(t, realPath(t, worktree.Dir), deletions[0].RemovesWorktree)
			require.Empty(t, deletions[0].LostFiles)
		})

		t.Run("lists the modified and untracked files that the removed worktree loses", func(t *testing.T) {
			repo := withEvents(t)
			worktree := repo.Worktree("events")
			worktree.Write("events.go", "package orders\n\ntype Placed struct{}\n")
			worktree.Write("notes.txt", "call the shop\n")

			deletions := check(t, repo)

			require.ElementsMatch(t, []string{"events.go", "notes.txt"}, deletions[0].LostFiles)
		})

		t.Run("marks a worktree whose folder is gone", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			require.NoError(t, os.RemoveAll(worktree))

			deletions := check(t, repo)

			require.Equal(t, stack.Deletion{Branch: deletions[0].Branch, RemovesWorktree: worktree, WorktreeGone: true}, deletions[0])
		})

		t.Run("the delete of a worktree closes the tmux panes in its folder, and no other pane", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			inWorktree := tmux.Pane{ID: "%1", Window: "work:events", Path: worktree}
			inSubfolder := tmux.Pane{ID: "%2", Window: "work:edit", Path: filepath.Join(worktree, "orders")}
			server := &memoryTmux{panes: []tmux.Pane{
				inWorktree,
				inSubfolder,
				{ID: "%3", Window: "work:shop", Path: realPath(t, repo.Dir)},
				{ID: "%4", Window: "work:copy", Path: worktree + "-copy"},
			}}

			deletions := checkWith(t, repo, server)

			require.Equal(t, []tmux.Pane{inWorktree, inSubfolder}, deletions[0].ClosesPanes)
		})

		t.Run("closes no pane when tmux cannot list its panes", func(t *testing.T) {
			repo := withEvents(t)
			repo.Worktree("events")

			deletions := checkWith(t, repo, &memoryTmux{panesErr: errors.New("no server running")})

			require.Empty(t, deletions[0].ClosesPanes)
		})

		t.Run("refuses the branch that strata runs on", func(t *testing.T) {
			repo := withEvents(t)
			repo.Switch("events")

			_, err := deleter(repo).Check(ctx, read(t, repo).Branches)

			require.EqualError(t, err, "events is checked out here: switch to another branch first")
		})

		t.Run("refuses a branch checked out in the main worktree while strata runs in another worktree", func(t *testing.T) {
			repo := withEvents(t)
			repo.Switch("events")
			billing := repo.WorktreeAtCommit("billing", "main")
			billing.Commit("billing.go", "package billing\n", "Add billing")
			tree := read(t, repo)
			events := tree.Branches[tree.Index("events")]

			_, err := stack.NewDeleter(git.New(billing.Dir), "origin/main", &memoryTmux{}).Check(ctx, []stack.Branch{events})

			require.EqualError(t, err, "events is checked out in the main worktree "+realPath(t, repo.Dir)+": switch it to another branch first")
		})

		t.Run("refuses a branch whose worktree is locked, and names the reason", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			repo.Git("worktree", "lock", "--reason", "on a USB disk", worktree)

			_, err := deleter(repo).Check(ctx, read(t, repo).Branches)

			require.EqualError(t, err, "the worktree "+worktree+" of events is locked (on a USB disk): unlock it with git worktree unlock first")
		})
	})

	t.Run("delete", func(t *testing.T) {
		t.Run("deletes each branch and its settings", func(t *testing.T) {
			repo := withEvents(t)
			repo.Switch("events")
			repo.Git("push", "-q", "-u", "origin", "events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")

			err := deleter(repo).Delete(ctx, check(t, repo))

			require.NoError(t, err)
			require.Equal(t, []string{"main"}, heads(t, repo))
			hasSettings, err := git.New(repo.Dir).Check(ctx, "config", "--get", "branch.events.remote")
			require.NoError(t, err)
			require.False(t, hasSettings)
		})

		t.Run("removes the worktree of the branch, and then the branch", func(t *testing.T) {
			repo := withEvents(t)
			worktree := repo.Worktree("events")

			err := deleter(repo).Delete(ctx, check(t, repo))

			require.NoError(t, err)
			require.NoDirExists(t, worktree.Dir)
			require.Equal(t, []string{"main"}, heads(t, repo))
		})

		t.Run("removes a worktree with the files that the check listed", func(t *testing.T) {
			repo := withEvents(t)
			worktree := repo.Worktree("events")
			worktree.Write("notes.txt", "call the shop\n")

			err := deleter(repo).Delete(ctx, check(t, repo))

			require.NoError(t, err)
			require.NoDirExists(t, worktree.Dir)
			require.Equal(t, []string{"main"}, heads(t, repo))
		})

		t.Run("removes the record of a worktree whose folder is gone", func(t *testing.T) {
			repo := withEvents(t)
			require.NoError(t, os.RemoveAll(repo.Worktree("events").Dir))

			err := deleter(repo).Delete(ctx, check(t, repo))

			require.NoError(t, err)
			require.Len(t, strings.Split(strings.TrimSpace(repo.Git("worktree", "list")), "\n"), 1)
			require.Equal(t, []string{"main"}, heads(t, repo))
		})

		t.Run("closes the tmux panes that the check listed", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			server := &memoryTmux{panes: []tmux.Pane{{ID: "%1", Window: "work:events", Path: worktree}}}

			err := deleterWith(repo, server).Delete(ctx, checkWith(t, repo, server))

			require.NoError(t, err)
			require.Equal(t, []string{"%1"}, server.closed)
		})

		t.Run("leaves a pane that left the worktree after the check", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			server := &memoryTmux{panes: []tmux.Pane{{ID: "%1", Window: "work:events", Path: worktree}}}
			deletions := checkWith(t, repo, server)
			server.panes[0].Path = realPath(t, repo.Dir)

			err := deleterWith(repo, server).Delete(ctx, deletions)

			require.NoError(t, err)
			require.Empty(t, server.closed)
		})

		t.Run("leaves a pane that opened in the worktree after the check", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			server := &memoryTmux{}
			deletions := checkWith(t, repo, server)
			server.panes = []tmux.Pane{{ID: "%1", Window: "work:events", Path: worktree}}

			err := deleterWith(repo, server).Delete(ctx, deletions)

			require.NoError(t, err)
			require.Empty(t, server.closed)
		})

		t.Run("deletes the branch when tmux cannot close a pane", func(t *testing.T) {
			repo := withEvents(t)
			worktree := realPath(t, repo.Worktree("events").Dir)
			server := &memoryTmux{panes: []tmux.Pane{{ID: "%1", Window: "work:events", Path: worktree}}}
			deletions := checkWith(t, repo, server)
			server.closeErr = errors.New("can't find pane: %1")

			err := deleterWith(repo, server).Delete(ctx, deletions)

			require.NoError(t, err)
			require.Equal(t, []string{"main"}, heads(t, repo))
		})

		t.Run("removes nothing when the worktree got new files after the check", func(t *testing.T) {
			repo := withEvents(t)
			worktree := repo.Worktree("events")
			deletions := check(t, repo)
			worktree.Write("notes.txt", "call the shop\n")

			err := deleter(repo).Delete(ctx, deletions)

			require.EqualError(t, err, "the worktree of events changed after strata showed the delete: press d again")
			require.FileExists(t, filepath.Join(worktree.Dir, "notes.txt"))
			require.Equal(t, []string{"events", "main"}, heads(t, repo))
		})

		t.Run("deletes nothing when a branch moved after the check", func(t *testing.T) {
			repo := withEvents(t)
			repo.Switch("events")
			repo.SwitchNew("handler")
			repo.Commit("handler.go", "package orders\n", "Add the handler")
			repo.Switch("main")
			deletions := check(t, repo)
			repo.Switch("handler")
			repo.Commit("handler.go", "package orders\n\ntype Handler struct{}\n", "Name the handler")
			repo.Switch("main")

			err := deleter(repo).Delete(ctx, deletions)

			require.ErrorContains(t, err, "strata deleted no branch")
			require.Equal(t, []string{"events", "handler", "main"}, heads(t, repo))
		})

		t.Run("refuses a branch that a worktree checked out after the check, and deletes nothing", func(t *testing.T) {
			repo := withEvents(t)
			deletions := check(t, repo)
			repo.Worktree("events")

			err := deleter(repo).Delete(ctx, deletions)

			require.EqualError(t, err, "the worktree of events changed after strata showed the delete: press d again")
			require.Equal(t, []string{"events", "main"}, heads(t, repo))
		})

		t.Run("refuses a branch that a rebase started to use after the check, and deletes nothing", func(t *testing.T) {
			repo := gittest.New(t)
			repo.SwitchNew("events")
			repo.Commit("README.md", "shop\n\nopen on weekdays\n", "Open on weekdays")
			repo.Switch("main")
			deletions := check(t, repo)
			worktree := repo.Worktree("events")
			repo.CommitOnOrigin("README.md", "shop\n\nopen all day\n", "Open all day")
			worktree.Git("fetch", "-q")
			worktree.StartRebase("origin/main")

			err := deleter(repo).Delete(ctx, deletions)

			require.ErrorContains(t, err, "uses events: finish or abort the rebase first")
			require.Equal(t, []string{"events", "main"}, heads(t, repo))
		})
	})
}
