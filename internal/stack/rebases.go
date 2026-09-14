package stack

import (
	"os"
	"path/filepath"
	"strings"
)

// RebaseWorktrees maps the ref of each branch that a rebase uses to the
// worktree of that rebase. A rebase detaches HEAD, so git for-each-ref does
// not name that worktree.
func RebaseWorktrees(common string) map[string]string {
	rebases := map[string]string{}
	read := func(gitDir, worktree string) {
		for _, state := range []string{"rebase-merge", "rebase-apply"} {
			if ref, err := os.ReadFile(filepath.Join(gitDir, state, "head-name")); err == nil {
				rebases[strings.TrimSpace(string(ref))] = worktree
			}
		}
	}
	read(common, mainWorktree(common))
	admin, _ := filepath.Glob(filepath.Join(common, "worktrees", "*"))
	for _, dir := range admin {
		if gitdir, err := os.ReadFile(filepath.Join(dir, "gitdir")); err == nil {
			read(dir, filepath.Dir(strings.TrimSpace(string(gitdir))))
		}
	}
	return rebases
}

func mainWorktree(common string) string {
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	return common
}
