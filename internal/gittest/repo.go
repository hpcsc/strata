package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type Repo struct {
	t      testing.TB
	Dir    string
	origin string
}

func New(t testing.TB) *Repo {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	dir := filepath.Join(root, "repo")
	run(t, root, "init", "-q", "--bare", "-b", "main", origin)
	run(t, root, "clone", "-q", origin, dir)
	r := &Repo{t: t, Dir: dir, origin: origin}
	r.Commit("README.md", "shop\n", "Start the shop")
	r.Git("push", "-q", "origin", "main")
	r.Git("remote", "set-head", "origin", "-a")
	return r
}

func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return run(r.t, r.Dir, args...)
}

func (r *Repo) Commit(path, content, message string) {
	r.t.Helper()
	r.Write(path, content)
	r.Git("add", "-A")
	r.Git("commit", "-q", "-m", message)
}

func (r *Repo) CommitOnOrigin(path, content, message string) {
	r.t.Helper()
	teammate := r.teammate()
	teammate.Commit(path, content, message)
	teammate.Git("push", "-q", "origin", "main")
}

func (r *Repo) SquashMergeOnOrigin(branch string) {
	r.t.Helper()
	teammate := r.teammate()
	teammate.Git("fetch", "-q", r.Dir, branch)
	teammate.Git("merge", "-q", "--squash", "FETCH_HEAD")
	teammate.Git("commit", "-q", "-m", "Merge "+branch)
	teammate.Git("push", "-q", "origin", "main")
}

func (r *Repo) teammate() *Repo {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), "teammate")
	run(r.t, filepath.Dir(dir), "clone", "-q", r.origin, dir)
	return &Repo{t: r.t, Dir: dir, origin: r.origin}
}

func (r *Repo) Write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.Dir, path)
	require.NoError(r.t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(r.t, os.WriteFile(full, []byte(content), 0o644))
}

func (r *Repo) SwitchNew(branch string) {
	r.t.Helper()
	r.Git("switch", "-q", "-c", branch)
}

func (r *Repo) Switch(branch string) {
	r.t.Helper()
	r.Git("switch", "-q", branch)
}

func (r *Repo) WorktreeAtCommit(branch, from string) *Repo {
	r.t.Helper()
	commit := strings.TrimSpace(r.Git("rev-parse", from))
	dir := r.worktreeDir(branch)
	r.Git("worktree", "add", "-q", "-b", branch, dir, commit)
	return &Repo{t: r.t, Dir: dir, origin: r.origin}
}

func (r *Repo) Worktree(branch string) *Repo {
	r.t.Helper()
	dir := r.worktreeDir(branch)
	r.Git("worktree", "add", "-q", dir, branch)
	return &Repo{t: r.t, Dir: dir, origin: r.origin}
}

func (r *Repo) worktreeDir(branch string) string {
	return filepath.Join(filepath.Dir(r.Dir), "worktree-"+strings.ReplaceAll(branch, "/", "-"))
}

func run(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=strata", "GIT_AUTHOR_EMAIL=strata@example.com",
		"GIT_COMMITTER_NAME=strata", "GIT_COMMITTER_EMAIL=strata@example.com",
		"GIT_CONFIG_COUNT=3",
		"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=core.hooksPath", "GIT_CONFIG_VALUE_1=/dev/null",
		"GIT_CONFIG_KEY_2=init.defaultBranch", "GIT_CONFIG_VALUE_2=main",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return string(out)
}
